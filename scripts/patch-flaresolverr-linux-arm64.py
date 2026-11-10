#!/usr/bin/env python3
"""Patch FlareSolverr for a bundled Linux arm64 Chromium/chromedriver.

- uc's multiprocessing Lock fails on macOS/some hosts; a thread lock is enough
  for one solver process.
- Prefer the chromedriver shipped next to the bundled Chrome, copied to a
  writable temp file because the AppImage mount is read-only.
"""
import pathlib
import sys

root = pathlib.Path(sys.argv[1])

p = root / 'src/undetected_chromedriver/patcher.py'
s = p.read_text().replace('from multiprocessing import Lock', 'from threading import Lock')
p.write_text(s)

p = root / 'src/utils.py'
s = p.read_text()
old = '''    # if we are inside the Docker container, we avoid downloading the driver
    driver_exe_path = None
    version_main = None
    if os.path.exists("/app/chromedriver"):
        # running inside Docker
        driver_exe_path = "/app/chromedriver"
    else:
        version_main = get_chrome_major_version()
        if PATCHED_DRIVER_PATH is not None:
            driver_exe_path = PATCHED_DRIVER_PATH

    # detect chrome path
    browser_executable_path = get_chrome_exe_path()
'''
new = '''    # detect chrome path first so a bundled chromedriver can sit beside it
    browser_executable_path = get_chrome_exe_path()
    driver_exe_path = None
    version_main = None
    bundled_driver = os.path.join(os.path.dirname(browser_executable_path), "chromedriver")
    if os.path.exists(bundled_driver):
        # The AppImage mount is read-only; uc patches the driver in place.
        dst = os.path.join(tempfile.gettempdir(), "godsend-chromedriver")
        if not os.path.exists(dst) or os.path.getsize(dst) != os.path.getsize(bundled_driver):
            shutil.copy2(bundled_driver, dst)
        os.chmod(dst, 0o755)
        driver_exe_path = dst
    elif os.path.exists("/app/chromedriver"):
        driver_exe_path = "/app/chromedriver"
    else:
        version_main = get_chrome_major_version()
        if PATCHED_DRIVER_PATH is not None:
            driver_exe_path = PATCHED_DRIVER_PATH
'''
if old not in s:
    raise SystemExit('webdriver block not found')
p.write_text(s.replace(old, new))
print('patched', root)
