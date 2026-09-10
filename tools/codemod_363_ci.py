from pathlib import Path

path = Path(__file__).resolve().parents[1] / "freenet-ui" / "web" / "operation-coordinator.js"
src = path.read_text()
marker = "// Issue #363: safe v0.3.8 visual polish without a DOM observer."
pos = src.find(marker)
if pos < 0:
    raise SystemExit("safe polish marker missing")
head, safe = src[:pos], src[pos:]
if "const previousFetch = window.fetch.bind(window);" not in safe:
    raise SystemExit("safe fetch wrapper anchor missing")
safe = safe.replace("const previousFetch = window.fetch.bind(window);", "const safeBaseFetch = window.fetch.bind(window);", 1)
safe = safe.replace("previousFetch(input, init)", "safeBaseFetch(input, init)")
path.write_text(head + safe)
