from pathlib import Path

p = Path('freenet-ui/web/operation-coordinator.js')
s = p.read_text(encoding='utf-8')
old = '''      @media(max-width:820px){.topbar.overview-approved{height:auto!important;min-height:64px!important}.best-v4-shell{grid-template-columns:1fr!important}.vpn-current-panel{padding-right:0!important}}\n'''
new = '''      @media(max-width:820px){.topbar.overview-approved{height:auto!important;min-height:64px!important;flex-wrap:wrap!important}.best-v4-shell{grid-template-columns:1fr!important}.vpn-current-panel{padding-right:0!important}#bestServerAdvanced.fn-topbar-vpn-picker{order:20!important;flex:1 0 100%!important;width:100%!important;max-width:none!important;min-width:0!important;margin:0!important}}\n      @media(max-width:560px){#bestServerAdvanced.fn-topbar-vpn-picker #profilesList.profiles{grid-template-columns:1fr!important;width:100%!important}}\n'''
if s.count(old) != 1:
    raise SystemExit(f'expected one mobile rule, found {s.count(old)}')
p.write_text(s.replace(old, new), encoding='utf-8')
for x in [Path('.github/workflows/issue-347-mobile-fix.yml'), Path('scripts/issue347_fix_mobile.py')]:
    if x.exists(): x.unlink()
