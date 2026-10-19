import os
import platform
import subprocess
import sys
import tarfile
import urllib.request
import zipfile

os.makedirs('dist', exist_ok=True)

os_name_map = {
    "darwin": "macos",
    "linux": "linux",
    "ubuntu": "linux",
    "windows": "windows"
}
arch_map = {
    "x86_64": "amd64",
    "arm64": "arm64",
    "aarch64": "arm64",
    "amd64": "amd64"
}
os_name = os_name_map[platform.system().lower()]
arch = arch_map[platform.machine().lower()]
print(f"Building for {os_name} {arch}")

HPROF_BASE = "https://github.com/parttimenerd/hprof-analyzer/releases/download/nightly"

# All platform binaries must exist before go build (go:embed requires them all).
hprof_targets = [
    ("hprof-analyzer-x86_64-unknown-linux-musl.tar.gz",  "hprof-analyzer-x86_64-unknown-linux-musl/hprof-redact",   "dist/hprof-redact-linux-amd64",      False),
    ("hprof-analyzer-aarch64-unknown-linux-musl.tar.gz", "hprof-analyzer-aarch64-unknown-linux-musl/hprof-redact",  "dist/hprof-redact-linux-arm64",      False),
    ("hprof-analyzer-aarch64-apple-darwin.tar.gz",       "hprof-analyzer-aarch64-apple-darwin/hprof-redact",        "dist/hprof-redact-darwin-arm64",     False),
    ("hprof-analyzer-x86_64-pc-windows-msvc.zip",        "hprof-analyzer-x86_64-pc-windows-msvc/hprof-redact.exe",  "dist/hprof-redact-windows-amd64.exe", True),
    ("hprof-analyzer-aarch64-pc-windows-msvc.zip",       "hprof-analyzer-aarch64-pc-windows-msvc/hprof-redact.exe", "dist/hprof-redact-windows-arm64.exe", True),
]

for archive_name, member, dest, is_zip in hprof_targets:
    if os.path.exists(dest) and os.path.getsize(dest) > 0:
        print(f"  {dest} already present, skipping")
        continue
    url = f"{HPROF_BASE}/{archive_name}"
    print(f"  Downloading {archive_name} -> {dest}")
    tmp = dest + ".tmp"
    try:
        urllib.request.urlretrieve(url, tmp)
        if is_zip:
            with zipfile.ZipFile(tmp) as zf:
                data = zf.read(member)
        else:
            with tarfile.open(tmp, "r:gz") as tf:
                data = tf.extractfile(member).read()
        with open(dest, "wb") as f:
            f.write(data)
        os.chmod(dest, 0o755)
    except Exception as e:
        # windows-arm64 may not always have a release; create an empty placeholder
        # so go:embed compiles (the plugin will report "not available" at runtime).
        print(f"  Warning: could not download {archive_name}: {e}; creating empty placeholder")
        open(dest, "wb").close()
    finally:
        if os.path.exists(tmp):
            os.remove(tmp)

if "--deps-only" in sys.argv:
    sys.exit(0)

exe = ".exe" if os_name == "windows" else ""
rc = subprocess.call(f"go build -o dist/cf-cli-java-plugin-{os_name}-{arch}{exe}", shell=True)
sys.exit(rc)
