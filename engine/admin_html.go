package engine

// adminHTML is the single-page operations console served at GET /admin. It is a
// self-contained HTML/JS file (no external assets) that talks to the /admin/api
// endpoints for testing, publishing, listing versions and rolling back rules.
const adminHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Rule Engine · Operations Console</title>
<style>
  :root { --bg:#0f1620; --card:#172230; --ink:#e6edf3; --muted:#8b98a5; --accent:#2f81f7; --ok:#2ea043; --bad:#f85149; }
  * { box-sizing: border-box; }
  body { margin:0; font:14px/1.5 -apple-system,Segoe UI,Roboto,sans-serif; background:var(--bg); color:var(--ink); }
  header { padding:16px 24px; border-bottom:1px solid #232f3e; font-weight:600; font-size:16px; }
  main { max-width:1000px; margin:0 auto; padding:24px; display:grid; gap:20px; }
  .card { background:var(--card); border:1px solid #232f3e; border-radius:10px; padding:18px; }
  h2 { margin:0 0 12px; font-size:14px; color:var(--muted); text-transform:uppercase; letter-spacing:.04em; }
  textarea, input { width:100%; background:#0d1117; color:var(--ink); border:1px solid #30363d; border-radius:6px; padding:10px; font-family:ui-monospace,Menlo,monospace; font-size:13px; }
  textarea { resize:vertical; }
  button { background:var(--accent); color:#fff; border:0; border-radius:6px; padding:8px 16px; font-weight:600; cursor:pointer; }
  button.ghost { background:#21262d; }
  button:hover { opacity:.9; }
  .row { display:flex; gap:12px; align-items:center; margin-top:10px; flex-wrap:wrap; }
  .grid2 { display:grid; grid-template-columns:1fr 1fr; gap:12px; }
  .result { margin-top:10px; padding:10px; border-radius:6px; font-family:ui-monospace,monospace; }
  .result.ok { background:rgba(46,160,67,.15); color:var(--ok); }
  .result.bad { background:rgba(248,81,73,.15); color:var(--bad); }
  table { width:100%; border-collapse:collapse; }
  th, td { text-align:left; padding:8px; border-bottom:1px solid #232f3e; font-size:13px; }
  .tag { font-size:11px; padding:2px 8px; border-radius:10px; background:#21262d; color:var(--muted); }
  .tag.active { background:rgba(46,160,67,.2); color:var(--ok); }
  .muted { color:var(--muted); font-size:12px; }
</style>
</head>
<body>
<header>⚙️ Rule Engine · Operations Console</header>
<main>

  <div class="card">
    <h2>1 · Test a rule</h2>
    <div class="grid2">
      <div>
        <div class="muted">Rule expression (SQL / CEL / Expr — matches the running backend)</div>
        <textarea id="t-expr" rows="3">age BETWEEN 25 AND 40 AND province IN ('广东','江苏') AND active_score >= 85</textarea>
      </div>
      <div>
        <div class="muted">Sample user (JSON)</div>
        <textarea id="t-user" rows="3">{"age":30,"province":"广东","active_score":92}</textarea>
      </div>
    </div>
    <div class="row"><button onclick="testRule()">Test</button></div>
    <div id="t-result"></div>
  </div>

  <div class="card">
    <h2>2 · Edit &amp; publish rules</h2>
    <div class="muted">Active rules (JSON array). Edit, add a note, then Publish to hot-reload the engine.</div>
    <textarea id="p-rules" rows="12"></textarea>
    <div class="row">
      <input id="p-note" placeholder="release note, e.g. add VIP rule" style="flex:1">
      <button onclick="publish()">Publish new version</button>
      <button class="ghost" onclick="loadRules()">Reload</button>
    </div>
    <div id="p-result"></div>
  </div>

  <div class="card">
    <h2>3 · Versions &amp; rollback</h2>
    <table id="v-table"><thead><tr><th>Version</th><th>Note</th><th>Created</th><th>Status</th><th></th></tr></thead><tbody></tbody></table>
  </div>

</main>
<script>
function show(id, ok, msg){ var e=document.getElementById(id); e.className='result '+(ok?'ok':'bad'); e.textContent=msg; }

function testRule(){
  var expr=document.getElementById('t-expr').value;
  var fields;
  try { fields=JSON.parse(document.getElementById('t-user').value); }
  catch(e){ show('t-result',false,'invalid user JSON: '+e.message); return; }
  fetch('/admin/api/test',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({expr:expr,fields:fields})})
    .then(function(r){return r.json();})
    .then(function(d){
      if(!d.ok){ show('t-result',false,'compile error: '+d.error); return; }
      show('t-result', d.matched, d.matched ? 'MATCH ✓ — the user satisfies this rule' : 'no match ✗ — the user does not satisfy this rule');
    });
}

function loadRules(){
  fetch('/admin/api/rules').then(function(r){return r.json();}).then(function(d){
    document.getElementById('p-rules').value=JSON.stringify(d.rules,null,2);
  });
  loadVersions();
}

function publish(){
  var rules;
  try { rules=JSON.parse(document.getElementById('p-rules').value); }
  catch(e){ show('p-result',false,'invalid rules JSON: '+e.message); return; }
  var note=document.getElementById('p-note').value||'(no note)';
  fetch('/admin/api/publish',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({rules:rules,note:note})})
    .then(function(r){return r.json();})
    .then(function(d){
      show('p-result',true,'published v'+d.version+' — loaded '+d.loaded+' rules, '+d.failed+' failed');
      loadVersions();
    });
}

function rollback(v){
  fetch('/admin/api/rollback',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({version:v})})
    .then(function(r){return r.json();})
    .then(function(d){ loadRules(); });
}

function loadVersions(){
  fetch('/admin/api/versions').then(function(r){return r.json();}).then(function(d){
    var tb=document.querySelector('#v-table tbody'); tb.innerHTML='';
    (d.versions||[]).forEach(function(v){
      var tr=document.createElement('tr');
      var status = v.active ? '<span class="tag active">active</span>' : '<span class="tag">archived</span>';
      var btn = v.active ? '' : '<button class="ghost" onclick="rollback('+v.version+')">Rollback</button>';
      tr.innerHTML='<td>v'+v.version+'</td><td>'+(v.note||'')+'</td><td class="muted">'+new Date(v.created_at).toLocaleString()+'</td><td>'+status+'</td><td>'+btn+'</td>';
      tb.appendChild(tr);
    });
  });
}

loadRules();
</script>
</body>
</html>`
