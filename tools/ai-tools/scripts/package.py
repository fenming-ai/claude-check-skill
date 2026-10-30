#!/usr/bin/env python3
from pathlib import Path
import hashlib,json,zipfile,re,html
root=Path(__file__).resolve().parent.parent
dist=root/'dist';dist.mkdir(exist_ok=True)
files=['manifest.json','popup.html','popup.js','check.js','redact.js','netcheck.js','style.css','README.md']
with zipfile.ZipFile(dist/'ai-tools-extension-0.1.0.zip','w',zipfile.ZIP_DEFLATED) as z:
 for name in files:
  info=zipfile.ZipInfo('extension/'+name,(2026,10,27,0,0,0));info.compress_type=zipfile.ZIP_DEFLATED;info.external_attr=0o644<<16;z.writestr(info,(root/'extension'/name).read_bytes())
for arch in ['arm64','amd64']:
 binary=dist/f'aitools-darwin-{arch}'
 if not binary.exists():continue
 with zipfile.ZipFile(dist/f'ai-tools-macos-{arch}.zip','w',zipfile.ZIP_DEFLATED) as z:
  info=zipfile.ZipInfo('AI Tools/aitools',(2026,10,27,0,0,0));info.compress_type=zipfile.ZIP_DEFLATED;info.external_attr=0o100755<<16;z.writestr(info,binary.read_bytes())
  launch=zipfile.ZipInfo('AI Tools/AI Tools.command',(2026,10,27,0,0,0));launch.compress_type=zipfile.ZIP_DEFLATED;launch.external_attr=0o100755<<16
  z.writestr(launch, '#!/bin/bash\ncd -- "$(dirname -- "$0")" || exit 1\nexec ./aitools\n')
  readme=zipfile.ZipInfo('AI Tools/使用说明.txt',(2026,10,27,0,0,0));readme.compress_type=zipfile.ZIP_DEFLATED;readme.external_attr=0o100644<<16
  z.writestr(readme,'双击 AI Tools.command，会打开终端并自动打开本机浏览器页面。浏览器没有打开或标签被关闭时，可从终端复制地址重新打开。页面内退出或终端 Ctrl+C 均可结束程序。未签名公证；若系统提示身份不明，确认来源与校验值后可由你本人在系统设置→隐私与安全性→仍要打开中决定是否允许此程序。不关闭全局安全保护。')
names={'ai-tools-extension-0.1.0.zip':'Chrome 扩展','ai-tools-macos-arm64.zip':'macOS · Apple Silicon','ai-tools-macos-amd64.zip':'macOS · Intel','aitools-windows-amd64.exe':'Windows · x64'}
rows=[];records=[]
for name,label in names.items():
 p=dist/name
 if not p.exists():continue
 digest=hashlib.sha256(p.read_bytes()).hexdigest();records.append(dict(file=name,sha256=digest,size=p.stat().st_size))
 rows.append(f'<article class="download"><h3>{label}</h3><a href="../dist/{name}" download>下载 {name}</a><p class="note">0.1.0-preview · {p.stat().st_size:,} 字节 · 完整实机验收未完成</p><p class="hash">SHA-256: {digest}</p></article>')
(dist/'checksums.json').write_text(json.dumps(records,ensure_ascii=False,indent=2)+'\n')
(dist/'SHA256SUMS').write_text(''.join(x['sha256']+'  '+x['file']+'\n' for x in records))
p=root/'site/download.html';s=p.read_text();s=re.sub(r'<!-- downloads:start -->.*?<!-- downloads:end -->','<!-- downloads:start -->'+''.join(rows)+'<!-- downloads:end -->',s,flags=re.S);p.write_text(s)
print('Packaged extension and updated 4 download entries')
