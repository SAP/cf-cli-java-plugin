import os
import platform

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

os.system(f"go build -o dist/cf-cli-java-plugin-{os_name}-{arch}")