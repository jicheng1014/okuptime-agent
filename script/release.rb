#!/usr/bin/env ruby

require "base64"
require "digest"
require "fileutils"
require "json"
require "open3"
require "openssl"
require "pathname"
require "time"
require "tmpdir"
require "uri"

ROOT = Pathname(__dir__).join("..").expand_path

def usage!
  abort <<~TEXT
    用法：
      ruby script/release.rb keygen /仓库外/cli-update.pem
      ruby script/release.rb release 0.1.0 /仓库外/cli-update.pem [发布说明]
    可选环境变量：CLI_RELEASE_NOTES、CLI_RELEASE_BASE_URL、CLI_RELEASE_OUTPUT_DIR
  TEXT
end

def outside_repository!(path)
  abort "私钥必须保存在仓库外" if path == ROOT || path.to_s.start_with?(ROOT.to_s + File::SEPARATOR)
end

command, argument, key_path, notes = ARGV
usage! unless %w[keygen release].include?(command)

if command == "keygen"
  usage! unless argument && ARGV.length == 2
  path = Pathname(argument).expand_path
  outside_repository!(path)
  path.dirname.mkpath
  path = path.dirname.realpath.join(path.basename)
  outside_repository!(path)
  abort "私钥已经存在" if path.exist?
  key = OpenSSL::PKey.generate_key("ED25519")
  path.open(File::WRONLY | File::CREAT | File::EXCL, 0o600) { |file| file.write(key.private_to_pem) }
  puts "已生成签名私钥：#{path}"
  exit
end

version = argument.to_s
usage! unless version.match?(/\A(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\z/) && version.split(".").all? { |part| part.to_i <= 4_294_967_295 } && key_path && ARGV.length.between?(3, 4)
key_file = Pathname(key_path).expand_path.realpath
outside_repository!(key_file)
abort "私钥权限必须为 0600" unless key_file.stat.mode & 0o777 == 0o600
private_key = OpenSSL::PKey.read(key_file.read)
abort "签名私钥必须使用 ED25519" unless private_key.oid == "ED25519"
public_key = Base64.strict_encode64(private_key.public_to_der)
base_url = ENV.fetch("CLI_RELEASE_BASE_URL", "https://www.okuptime.com/cli/releases").delete_suffix("/")
uri = URI(base_url)
abort "发布地址必须使用干净的 HTTPS 地址" unless uri.is_a?(URI::HTTPS) && uri.host && !uri.userinfo && !uri.query && !uri.fragment
release_root = Pathname(ENV.fetch("CLI_RELEASE_OUTPUT_DIR", ROOT.join("dist/releases").to_s)).expand_path
release_dir = release_root.join("v#{version}")
abort "版本目录已经存在，版本文件不可覆盖：#{release_dir}" if release_dir.exist?
release_root.mkpath

Dir.mktmpdir(".cli-release-", release_root.to_s) do |directory|
  staged_release = Pathname(directory).join("release")
  staged_release.mkpath
  artifacts = %w[darwin linux windows].product(%w[amd64 arm64]).map do |platform, architecture|
    suffix = platform == "windows" ? ".exe" : ""
    binary = staged_release.join("okuptime-#{platform}-#{architecture}#{suffix}")
    stdout, stderr, status = Open3.capture3(
      { "CGO_ENABLED" => "0", "GOOS" => platform, "GOARCH" => architecture },
      "go", "build", "-trimpath", "-ldflags", "-X main.version=#{version} -X main.updatePublicKey=#{public_key}",
      "-o", binary.to_s, "./cmd/okuptime", chdir: ROOT.to_s
    )
    abort [ stdout, stderr ].join unless status.success?

    sha256 = Digest::SHA256.file(binary).hexdigest
    filename = "okuptime-v#{version}-#{platform}-#{architecture}-#{sha256[0, 12]}#{suffix}"
    File.rename(binary, staged_release.join(filename))
    url = "#{base_url}/v#{version}/#{filename}"
    size = staged_release.join(filename).size
    payload = [ version, platform, architecture, url, sha256, size, "" ].join("\n")
    { version: version, platform: platform, architecture: architecture, url: url, sha256: sha256, size: size,
      signature: Base64.strict_encode64(private_key.sign(nil, payload)) }
  end
  manifest = { version: version, released_at: Time.now.utc.iso8601, notes: notes || ENV.fetch("CLI_RELEASE_NOTES", ""), artifacts: artifacts }
  json = JSON.pretty_generate(manifest) + "\n"
  staged_release.join("manifest.json").write(json)
  latest = Pathname(directory).join("latest.json")
  latest.write(json)
  File.rename(staged_release, release_dir)
  File.rename(latest, release_root.join("latest.json"))
  puts "已生成 CLI #{version} 的 #{artifacts.length} 个平台发布文件：#{release_dir}"
end
