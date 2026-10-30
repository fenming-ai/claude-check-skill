from pathlib import Path
from html.parser import HTMLParser
import hashlib,json,zipfile
root=Path(__file__).resolve().parent.parent
class Check(HTMLParser):
 def handle_starttag(self,tag,attrs):
  attrs=dict(attrs)
  for k in ['href','src']:
   if k in attrs:
    v=attrs[k];assert not v.startswith(('http:','https:','//')),v
    if not v.startswith('#'):assert (self.base/v).exists(),v
for p in (root/'site').glob('*.html'):
 c=Check();c.base=p.parent;c.feed(p.read_text());assert '<script' not in p.read_text()
records=json.loads((root/'dist/checksums.json').read_text());assert len(records)==4
for r in records:
 p=root/'dist'/r['file'];assert p.stat().st_size==r['size'];assert hashlib.sha256(p.read_bytes()).hexdigest()==r['sha256'];assert r['sha256'] in (root/'site/download.html').read_text()
with zipfile.ZipFile(root/'dist/ai-tools-extension-0.1.0.zip') as z:
 assert len(z.namelist())==8
 assert all(n.startswith('extension/') and not any(x in n for x in ['test','private','.git']) for n in z.namelist())
for arch in ['arm64','amd64']:
 with zipfile.ZipFile(root/f'dist/ai-tools-macos-{arch}.zip') as z:
  executable=z.getinfo('AI Tools/aitools');assert executable.external_attr>>16&0o111
  assert z.getinfo('AI Tools/AI Tools.command').external_attr>>16&0o111
  assert z.read(executable)==(root/f'dist/aitools-darwin-{arch}').read_bytes()
print('PASS: 6 static pages, local links, 4 checksums, extension allowlist, macOS clickable launchers')

import re
private_key=re.compile(rb'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----')
api_key=re.compile(rb'sk-ant-[A-Za-z0-9_-]{16,}')
for base in ['extension','local','site','dist']:
 for p in (root/base).rglob('*'):
  if p.is_file():
   content=p.read_bytes();assert not private_key.search(content),p;assert not api_key.search(content),p
   if p.suffix=='.zip':
    with zipfile.ZipFile(p) as archive:
     for name in archive.namelist():
      body=archive.read(name);assert not private_key.search(body),(p,name);assert not api_key.search(body),(p,name)
print('PASS: no private key blocks or Anthropic credential patterns in source/packages')
