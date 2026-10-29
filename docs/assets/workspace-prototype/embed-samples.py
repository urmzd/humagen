from pathlib import Path
import base64
import json
import re

root = Path(__file__).resolve().parent
page = root / 'index.html'
data = {file.name: base64.b64encode(file.read_bytes()).decode()
        for file in sorted((root / 'exports').iterdir()) if file.is_file()}
body, count = re.subn(
    r'(<script type="application/json" id="download-data">).*?(</script>)',
    lambda match: match[1] + json.dumps(data) + match[2],
    page.read_text(), flags=re.S)
assert count == 1, 'Expected one embedded download data block'
page.write_text(body)
print(f'Embedded {len(data)} sample files in index.html')
