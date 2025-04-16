import os
import platform

os.makedirs('dist', exist_ok=True)

os_name_map = {
    "Darwin": "macos",
    "Linux": "linux",
    "Windows": "windows"
}
os_name = os_name_map[platform.system()].lower()
arch = platform.machine()    # e.g., 'x86_64', 'arm64'
print(f"Building for {os_name} {arch}")

os.system(f"go build -o dist/cf-cli-java-plugin-{os_name}-{arch}")