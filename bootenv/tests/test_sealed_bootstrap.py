import hashlib
import importlib.machinery
import importlib.util
from pathlib import Path
import tempfile
import unittest
import yaml
from jinja2 import Environment, StrictUndefined

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import ed25519, rsa


def load(name):
    path = Path(__file__).resolve().parents[1] / "scripts" / name
    loader = importlib.machinery.SourceFileLoader(name, str(path))
    spec = importlib.util.spec_from_loader(name, loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


identity = load("gomi-preserve-identity")
merger = load("gomi-merge-bootstrap")


class IdentityTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.rsa = rsa.generate_private_key(public_exponent=65537, key_size=2048)
        cls.ed = ed25519.Ed25519PrivateKey.generate()
        der = cls.rsa.public_key().public_bytes(
            serialization.Encoding.DER, serialization.PublicFormat.SubjectPublicKeyInfo)
        cls.fingerprint = hashlib.sha256(der).hexdigest()

    def write_identity(self, root):
        directory = root / "etc/ssh"
        directory.mkdir(parents=True)
        for name, key in (("rsa", self.rsa), ("ed25519", self.ed)):
            value = key.private_bytes(serialization.Encoding.PEM,
                                      serialization.PrivateFormat.OpenSSH,
                                      serialization.NoEncryption())
            (directory / ("ssh_host_" + name + "_key")).write_bytes(value)

    def test_preserves_matching_identity_and_permissions(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "root"
            output = Path(directory) / "ram/identity"
            self.write_identity(root)
            self.assertTrue(identity.preserve(root, output, self.fingerprint))
            self.assertEqual(output.stat().st_mode & 0o777, 0o700)
            for name in ("rsa", "ed25519"):
                path = "ssh_host_" + name + "_key"
                self.assertEqual((output / path).read_bytes(), (root / "etc/ssh" / path).read_bytes())
                self.assertEqual((output / path).stat().st_mode & 0o777, 0o600)
                self.assertTrue((output / (path + ".pub")).read_bytes().startswith(b"ssh-"))

    def test_wrong_or_missing_identity_cannot_stage_bootstrap(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "root"
            output = Path(directory) / "ram/identity"
            self.assertFalse(identity.preserve(root, output, self.fingerprint))
            self.write_identity(root)
            self.assertFalse(identity.preserve(root, output, "0" * 64))
            self.assertFalse(output.exists())

    def test_corrupt_optional_key_does_not_publish_partial_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "root"
            output = Path(directory) / "ram/identity"
            self.write_identity(root)
            (root / "etc/ssh/ssh_host_ed25519_key").write_text("invalid")
            with self.assertRaises(ValueError):
                identity.preserve(root, output, self.fingerprint)
            self.assertFalse(output.exists())

    def test_symlink_escape_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "root"
            outside = Path(directory) / "outside"
            self.write_identity(outside)
            root.mkdir()
            (root / "etc").symlink_to(outside / "etc")
            self.assertFalse(identity.preserve(root, Path(directory) / "output", self.fingerprint))


class MergeTests(unittest.TestCase):
    def test_cloud_init_can_render_serialized_embedded_kubeadm_jinja(self):
        # Real CABPK embeds a multiline kubeadm configuration in write_files.
        # Default PyYAML serialization wrapped an expression with a backslash,
        # causing cloud-init to silently ignore the entire bootstrap document.
        content = ("apiVersion: kubeadm.k8s.io/v1beta4\nnodeRegistration:\n"
                   "  name: '{{ v1.local_hostname }}'\n  kubeletExtraArgs:\n"
                   "  - name: provider-id\n    value: gomi:///{{ v1.instance_id }}\n")
        callback = "# gomi-capi-completion\nnotify /install-complete"
        config = merger.merge({"runcmd": [callback], "ssh_authorized_keys": ["ssh-ed25519 public"]},
                              {"write_files": [{"path": "/run/kubeadm.yaml", "content": content}],
                               "runcmd": ["echo {{ v1['local_hostname'] }}", "x" * 150 + " {{ v1.instance_id }}"]})
        rendered = Environment(undefined=StrictUndefined).from_string(merger.serialize(config, jinja=True)).render(
            v1={"local_hostname": "node1", "instance_id": "capi-owner"})
        result = yaml.safe_load(rendered)
        kubeadm = yaml.safe_load(result["write_files"][0]["content"])
        self.assertEqual(kubeadm["nodeRegistration"]["name"], "node1")
        self.assertEqual(kubeadm["nodeRegistration"]["kubeletExtraArgs"][0]["value"], "gomi:///capi-owner")
        self.assertEqual(result["runcmd"][0], "echo node1")
        self.assertEqual(result["ssh_authorized_keys"], ["ssh-ed25519 public"])
        self.assertEqual(yaml.safe_load(merger.serialize(config)), config)

    def test_jinja_boundary_trimming_cannot_consume_yaml_structure(self):
        env = Environment(undefined=StrictUndefined, keep_trailing_newline=True)
        cases = [
            "{{- value }}", "  \n{{- value }}", "{{ value -}}\n\n",
            "{% if true -%}echo hello{%- endif %}",
            "{%- if true %}echo hello{% endif -%}\n",
            "{%- if false %}unused{% endif -%}",
            "{#- comment -#}", "{{- 'quoted' -}}", "echo {{- value }}",
            "a\n  {{- value -}}\n b", "{{ value }}\n\n",
        ]
        for value in cases:
            with self.subTest(value=value):
                config = {"runcmd": [value, "next-command"], "hostname": "unchanged"}
                result = yaml.safe_load(env.from_string(merger.serialize(config, jinja=True)).render(value="hello"))
                self.assertEqual(result["runcmd"], [env.from_string(value).render(value="hello"), "next-command"])
                self.assertEqual(result["hostname"], "unchanged")
                self.assertEqual(yaml.safe_load(merger.serialize(config)), config)

    def test_completion_is_after_bootstrap_and_gomi_setup_survives(self):
        callback = "# gomi-capi-completion\nnotify /install-complete"
        base = {"hostname": "node1", "runcmd": ["network-setup", callback, "wol-setup"],
                "write_files": [{"path": "/gomi"}]}
        bootstrap = {"runcmd": ["kubeadm init", "touch /run/cluster-api/bootstrap-success.complete"],
                     "write_files": [{"path": "/etc/kubernetes/pki/ca.key", "content": "private"}]}
        result = merger.merge(base, bootstrap)
        self.assertEqual(result["hostname"], "node1")
        self.assertEqual(result["runcmd"][:4], ["network-setup", "wol-setup", "kubeadm init",
                                              "touch /run/cluster-api/bootstrap-success.complete"])
        self.assertIn("|| exit 1", result["runcmd"][-2])
        self.assertEqual(result["runcmd"][-1], callback)
        self.assertFalse(result["ssh_deletekeys"])
        self.assertEqual(len(result["write_files"]), 2)
        self.assertEqual(len(base["write_files"]), 1)

    def test_missing_or_duplicate_callback_fails_closed(self):
        for commands in ([], ["unexpected"], ["# gomi-capi-completion\na"] * 2):
            with self.assertRaises(ValueError):
                merger.merge({"runcmd": commands}, {"runcmd": ["kubeadm init"]})


if __name__ == "__main__":
    unittest.main()
