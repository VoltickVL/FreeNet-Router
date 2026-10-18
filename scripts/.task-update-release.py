from pathlib import Path
p=Path('.github/workflows/release.yml');s=p.read_text()
a='          cp config/freenet.conf.example dist/freenet.conf.example\n'
b=a+'''
          # Capabilities are part of the exact release SHA manifest, not inferred
          # from a newer version number or mutable REST metadata.
          python3 - <<'PYMETA'
          import json, pathlib
          tag = pathlib.Path('VERSION').read_text().strip()
          notes = pathlib.Path('docs/releases/' + tag + '.md').read_text()
          pathlib.Path('dist/recovery-protocol.json').write_text(json.dumps({'tag':tag,'protocol':2})+'\\n')
          pathlib.Path('dist/release-metadata.json').write_text(json.dumps({'tag_name':tag,'draft':False,'prerelease':'-beta.' in tag,'body':notes},ensure_ascii=False)+'\\n')
          PYMETA
'''
assert s.count(a)==1;s=s.replace(a,b)
a='              self_update.sh \\\n';b=a+'              recovery-protocol.json \\\n              release-metadata.json \\\n'
assert s.count(a)==1;s=s.replace(a,b)
Path('.github/next-release.yml').write_text(s)

# Own the in-process slot as well as the helper's cross-process network fence.
# Reserve after duplicate-update handling so repeat clicks attach to progress.
p=Path('freenet-ui/main.go');s=p.read_text();start=s.index('func (a *app) handleSelfUpdateApply(');end=s.index('\nfunc readStateFile',start)
b=s[start:end]
a='\tcmd := a.selfUpdateCommand("apply", req.TargetTag)'
b=b.replace(a,'''\treleaseSlot, reserved := a.reserveApplicationUpdateSlot()
	if !reserved {
		a.updateMu.Unlock()
		writeJSON(w, http.StatusConflict, actionResult{Success: false, Error: "Другая операция FreeNet ещё выполняется; обновление не начато."})
		return
	}
'''+a)
a='\tif err := cmd.Start(); err != nil {\n';assert b.count(a)==1;b=b.replace(a,a+'\t\treleaseSlot()\n')
a='\t\t_ = cmd.Wait()\n';assert b.count(a)==1;b=b.replace(a,a+'\t\treleaseSlot()\n')
s=s[:start]+b+s[end:];p.write_text(s)
p=Path('freenet-ui/self_update_recovery.go');s=p.read_text()
a='\ta.updateLaunching = true\n\ta.updateTarget = "recovery"'
b='''\treleaseSlot, reserved := a.reserveApplicationUpdateSlot()
	if !reserved {
		a.updateMu.Unlock()
		writeJSON(w, http.StatusConflict, actionResult{Success: false, Error: "Другая операция FreeNet ещё выполняется; Recovery не начал изменений."})
		return
	}
'''+a
assert s.count(a)==1;s=s.replace(a,b)
a='\t\ta.releaseRecoveryLaunch()\n';assert s.count(a)==3;s=s.replace(a,'\t\treleaseSlot()\n'+a)
p.write_text(s)
