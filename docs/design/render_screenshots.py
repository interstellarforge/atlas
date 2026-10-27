"""Render self-contained Atlas mockups. Dev-only: pip install playwright + Chromium.
Production CSS and HTML have zero runtime dependencies.
We inject CSS & demo locale to avoid needing a running web server.
"""
from pathlib import Path
import json, base64
from playwright.sync_api import sync_playwright
ROOT=Path(__file__).resolve().parent
OUT=ROOT/'screenshots'; OUT.mkdir(exist_ok=True)
CSS='\n'.join((ROOT/name).read_text() for name in ['tokens.css','base.css','components.css','layouts.css'])
JS=(ROOT/'demo/app.js').read_text().replace("const L = await (await fetch('./en.json')).json();",'const L = '+json.dumps(json.loads((ROOT/'demo/en.json').read_text()))+';')
LOGO='data:image/svg+xml;base64,'+base64.b64encode((ROOT/'assets/atlas-mark.svg').read_bytes()).decode()
HTML='<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head><body style="margin:0"><div id="app" class="atlas-root"></div></body></html>'
with sync_playwright() as pw:
    browser=pw.chromium.launch(headless=True,executable_path='/usr/bin/chromium',args=['--no-sandbox'])
    checks=[]
    for screen in ['board','card','list','detail','login']:
        for theme in ['dark','light']:
            for viewport,width,height in [('desktop',1440,960),('mobile-375',375,812)]:
                page=browser.new_page(viewport={'width':width,'height':height},device_scale_factor=1)
                errs=[]
                page.on('pageerror',lambda err:errs.append(str(err)))
                # app.js reads location.search, so set initial history query using mock origin.
                page.set_content(HTML,wait_until='domcontentloaded')
                page.add_style_tag(content=CSS)
                page.add_script_tag(content=JS.replace("const qs = new URLSearchParams(location.search);", f"const qs = new URLSearchParams('?view={screen}&theme={theme}');"))
                page.locator('img[src="../assets/atlas-mark.svg"]').evaluate_all('(images,src)=>images.forEach(i=>i.src=src)',LOGO)
                page.wait_for_timeout(70)
                overflow=page.evaluate('document.documentElement.scrollWidth > window.innerWidth')
                first_card=page.locator('.atlas-card').first
                h=first_card.evaluate('(e)=>e.getBoundingClientRect().height') if first_card.count() else None
                if errs: raise RuntimeError(f'{screen}/{theme}/{viewport}: {errs}')
                if h is not None and abs(h-88)>0.1: raise RuntimeError(f'Invalid card height: {h}')
                filename=OUT/f'atlas-{screen}-{theme}-{viewport}.png'
                page.screenshot(path=str(filename),full_page=(screen != 'detail' or viewport != 'mobile-375'),animations='disabled')
                checks.append((filename.name,overflow,page.locator('body').evaluate('(e)=>e.scrollHeight'),h))
                page.close()
    browser.close()
for entry in checks: print(*entry)
