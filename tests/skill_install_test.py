"""Offline bootstrap behavior checks; no user installation or network writes."""
import hashlib, json, os, pathlib, shutil, subprocess, tempfile, unittest
SKILL = pathlib.Path(__file__).resolve().parents[1] / 'skills/okuptime'
SCRIPT = SKILL / 'scripts/install.sh'
BINARY = b'#!/bin/sh\nprintf \'{"data":{"version":"0.1.1"}}\\n\'\n'
class InstallerTest(unittest.TestCase):
    def run_install(self, change=None, existing=False, system='Linux', arch='x86_64', status='200', download_status='200', script=SCRIPT):
        with tempfile.TemporaryDirectory() as root:
            root = pathlib.Path(root); home = root / 'home'; tools = root / 'tools'; tools.mkdir(); home.mkdir()
            meta = dict(version='0.1.1', platform='darwin' if system == 'Darwin' else 'linux', architecture='arm64' if arch == 'arm64' else 'amd64', url='https://www.okuptime.com/cli/releases/v0.1.1/okuptime', sha256=hashlib.sha256(BINARY).hexdigest(), size=len(BINARY))
            if change: meta.update(change)
            (root/'metadata').write_text(json.dumps({'data': meta}))
            (root/'binary').write_bytes(BINARY)
            (tools/'uname').write_text('#!/bin/sh\ncase "$1" in -s) echo '+system+';; -m) echo '+arch+';; esac\n')
            (tools/'curl').write_text('#!/bin/sh\nurl=""; output=""\nprintf "%s\\n" "$@" >> "$TEST_ROOT/requests"\nwhile [ "$#" -gt 0 ]; do case "$1" in https:*) url="$1";; -o) shift; output="$1";; esac; shift; done\ncase "$url" in *api/v1*) cp "$TEST_ROOT/metadata" "$output"; printf "$HTTP_STATUS";; *) cp "$TEST_ROOT/binary" "$output"; printf "$DOWNLOAD_STATUS";; esac\n')
            for tool in tools.iterdir(): tool.chmod(0o755)
            target = home/'.local/bin/okuptime'
            if existing: target.parent.mkdir(parents=True); target.write_bytes(BINARY); target.chmod(0o755)
            env = dict(os.environ, HOME=str(home), TEST_ROOT=str(root), HTTP_STATUS=status, DOWNLOAD_STATUS=download_status, OKUPTIME_TOKEN='do-not-expose-test-token', PATH=str(tools)+':/usr/bin:/bin')
            result = subprocess.run(['sh', str(script)], env=env, capture_output=True)
            self.requests = (root/'requests').read_text().splitlines() if (root/'requests').exists() else []
            self.assertNotIn(b'do-not-expose-test-token', result.stdout + result.stderr)
            return result, target.read_bytes() if target.exists() else None
    def test_standalone_skill_package(self):
        with tempfile.TemporaryDirectory() as root:
            installed = pathlib.Path(root) / 'okuptime'
            shutil.copytree(SKILL, installed)
            self.assertEqual({str(path.relative_to(installed)) for path in installed.rglob('*') if path.is_file()}, {'SKILL.md', 'scripts/install.sh', 'scripts/install.ps1'})
            result, binary = self.run_install(script=installed / 'scripts/install.sh')
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(binary, BINARY)
    def test_linux_install(self):
        result, binary = self.run_install(); self.assertEqual(result.returncode, 0, result.stderr); self.assertEqual(binary, BINARY)
    def test_downloads_from_github_with_https_redirects(self):
        result, binary = self.run_install()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('https://github.com/jicheng1014/okuptime-agent/releases/download/v0.1.1/okuptime', self.requests)
        self.assertNotIn('https://www.okuptime.com/cli/releases/v0.1.1/okuptime', self.requests)
        self.assertIn('--location', self.requests)
        self.assertEqual(self.requests[self.requests.index('--proto-redir') + 1], '=https')
        self.assertEqual(self.requests[self.requests.index('--max-redirs') + 1], '5')
    def test_rejects_failed_binary_download(self):
        for status in ['302', '404', '500']:
            with self.subTest(status=status):
                result, binary = self.run_install(download_status=status)
                self.assertNotEqual(result.returncode, 0)
                self.assertIsNone(binary)
    def test_mac_arm_install(self):
        result, binary = self.run_install(system='Darwin', arch='arm64'); self.assertEqual(result.returncode, 0, result.stderr); self.assertEqual(binary, BINARY)
    def test_rejects_redirect(self):
        result, binary = self.run_install(status="302"); self.assertNotEqual(result.returncode, 0); self.assertIsNone(binary)
    def test_reuses_existing(self):
        result, binary = self.run_install({'sha256': '0'*64}, existing=True); self.assertEqual(result.returncode, 0); self.assertEqual(binary, BINARY)
    def test_rejects_invalid_release(self):
        for change in [{'sha256':'0'*64}, {'size':len(BINARY)+1}, {'size':67108865}, {'platform':'windows'}, {'architecture':'arm64'}, {'url':'https://evil.example/cli'}, {'url':'https://www.okuptime.com/cli/releases/../../evil'}, {'version':'broken'}, {'version':'0.1.2'}, {'size':0}]:
            with self.subTest(change=change):
                result, binary = self.run_install(change); self.assertNotEqual(result.returncode, 0); self.assertIsNone(binary)
if __name__ == '__main__': unittest.main()
