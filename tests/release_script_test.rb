require "minitest/autorun"
require "base64"
require "digest"
require "json"
require "open3"
require "openssl"
require "pathname"
require "rbconfig"
require "tmpdir"

class ReleaseCliTest < Minitest::Test
  SCRIPT = File.expand_path("../script/release.rb", __dir__)

  def test_signatures_immutable_versions_and_failed_build_preserve_latest
    Dir.mktmpdir("cli-release-test") do |directory|
      key_file = File.join(directory, "key.pem")
      output = File.join(directory, "releases")
      fake_go = File.join(directory, "go")
      File.write(fake_go, <<~RUBY)
        #!#{RbConfig.ruby}
        abort "simulated failed build" if ENV["FAIL_PLATFORM"] == ENV["GOOS"]
        File.write(ARGV[ARGV.index("-o") + 1], ENV.fetch("GOOS") + "/" + ENV.fetch("GOARCH"))
      RUBY
      File.chmod(0o755, fake_go)
      environment = { "PATH" => "#{directory}:#{ENV.fetch('PATH')}", "CLI_RELEASE_OUTPUT_DIR" => output }

      _, error, status = run_script(environment, "keygen", key_file)
      assert status.success?, error
      assert_equal 0o600, File.stat(key_file).mode & 0o777
      _, _, status = run_script(environment, "keygen", key_file)
      refute status.success?
      _, error, status = run_script(environment, "release", "0.0.1", key_file, "test release")
      assert status.success?, error
      latest = File.join(output, "latest.json")
      previous = File.read(latest)
      manifest = JSON.parse(previous)
      assert_equal %w[darwin linux windows].product(%w[amd64 arm64]), manifest.fetch("artifacts").map { |artifact| artifact.values_at("platform", "architecture") }
      assert_equal "test release", manifest.fetch("notes")
      key = OpenSSL::PKey.read(File.read(key_file))
      manifest.fetch("artifacts").each do |artifact|
        binary = File.join(output, "v0.0.1", File.basename(artifact.fetch("url")))
        assert_equal artifact.fetch("sha256"), Digest::SHA256.file(binary).hexdigest
        assert_equal artifact.fetch("size"), File.size(binary)
        assert_equal artifact.fetch("platform") == "windows", binary.end_with?(".exe")
        payload = artifact.values_at("version", "platform", "architecture", "url", "sha256", "size").push("").join("\n")
        assert key.verify(nil, Base64.strict_decode64(artifact.fetch("signature")), payload)
      end

      _, _, status = run_script(environment, "release", "0.0.1", key_file)
      refute status.success?
      assert_equal previous, File.read(latest)
      _, _, status = run_script(environment.merge("FAIL_PLATFORM" => "windows"), "release", "0.0.2", key_file)
      refute status.success?
      assert_equal previous, File.read(latest)
      refute File.exist?(File.join(output, "v0.0.2"))
      assert_empty Dir.glob(File.join(output, ".cli-release-*"))

      _, _, status = run_script(environment.merge("CLI_RELEASE_BASE_URL" => "https://user:secret@example.com/releases"), "release", "0.0.3", key_file)
      refute status.success?
      _, _, status = run_script(environment, "release", "4294967296.0.0", key_file)
      refute status.success?
      _, _, status = run_script(environment, "release", "00.0.3", key_file)
      refute status.success?
      File.chmod(0o644, key_file)
      _, _, status = run_script(environment, "release", "0.0.3", key_file)
      refute status.success?
      assert_equal previous, File.read(latest)
    end
  end

  private

  def run_script(environment, *arguments)
    Open3.capture3(environment, RbConfig.ruby, SCRIPT, *arguments)
  end
end
