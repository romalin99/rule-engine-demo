// Package web holds the static operations console (step 11): a single-file rule
// editor that talks to the engine's HTTP API to edit, test, publish, list and
// roll back rules. It is served at GET "/" by engine.Serve.
package web

// Page is the self-contained rule-editor HTML (no external assets).
const Page = `<!doctype html>
<html lang="zh">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Rule Engine 运营控制台</title>
<style>
  :root{--bg:#0f1117;--card:#1a1d27;--line:#2a2f3c;--fg:#e6e8ee;--mut:#8b93a7;--ok:#3fb950;--no:#f85149;--acc:#388bfd}
  *{box-sizing:border-box}
  body{margin:0;background:var(--bg);color:var(--fg);font:14px/1.5 system-ui,Segoe UI,Roboto,Arial}
  header{padding:14px 20px;border-bottom:1px solid var(--line);font-weight:600}
  main{display:grid;grid-template-columns:1fr 1fr;gap:16px;padding:20px;max-width:1200px;margin:0 auto}
  .card{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:16px}
  h2{margin:0 0 12px;font-size:15px}
  label{display:block;color:var(--mut);margin:8px 0 4px}
  input,textarea{width:100%;background:#0c0e14;border:1px solid var(--line);color:var(--fg);border-radius:7px;padding:8px;font:13px/1.4 ui-monospace,Menlo,monospace}
  textarea{min-height:84px;resize:vertical}
  .row{display:flex;gap:10px}.row>div{flex:1}
  button{background:var(--acc);color:#fff;border:0;border-radius:7px;padding:8px 14px;cursor:pointer;font-weight:600}
  button.ghost{background:#222633;color:var(--fg);border:1px solid var(--line)}
  button.danger{background:#3a1d20;color:var(--no);border:1px solid #5a2329}
  .bar{display:flex;gap:8px;margin-top:12px;flex-wrap:wrap}
  table{width:100%;border-collapse:collapse;margin-top:8px}
  th,td{text-align:left;padding:6px 8px;border-bottom:1px solid var(--line);font-size:13px;vertical-align:top}
  th{color:var(--mut);font-weight:500}
  code{color:#a5d6ff}
  .pill{padding:1px 8px;border-radius:999px;font-size:12px}
  .pill.ok{background:#12351c;color:var(--ok)}.pill.no{background:#3a1115;color:var(--no)}
  #out{margin-top:12px;padding:10px;border-radius:7px;background:#0c0e14;border:1px solid var(--line);white-space:pre-wrap;min-height:20px}
  .mut{color:var(--mut)}
</style>
</head>
<body>
<header>⚙️ Rule Engine 运营控制台 <span class="mut">— 编辑 / 测试 / 发布 / 版本 / 回滚</span></header>
<main>
  <section class="card">
    <h2>规则编辑器</h2>
    <div class="row">
      <div><label>规则 ID</label><input id="rid" type="number" value="9001"></div>
      <div><label>优先级</label><input id="rprio" type="number" value="0"></div>
    </div>
    <label>名称</label><input id="rname" value="VIP用户">
    <label>表达式 (SQL WHERE 语法)</label>
    <textarea id="rexpr">age BETWEEN 25 AND 40 AND province IN ('广东','江苏') AND active_score >= 85</textarea>
    <label>测试样本 (JSON 行)</label>
    <textarea id="rrow">{"age":30,"province":"广东","active_score":96}</textarea>
    <div class="bar">
      <button onclick="testRule()">测试</button>
      <button class="ghost" onclick="publishRule()">发布规则（生效）</button>
      <button class="ghost" onclick="snapshot()">快照版本</button>
    </div>
    <div id="out" class="mut">结果会显示在这里…</div>
  </section>

  <section class="card">
    <h2>当前规则 <button class="ghost" style="float:right;padding:4px 10px" onclick="loadRules()">刷新</button></h2>
    <table><thead><tr><th>ID</th><th>名称</th><th>表达式</th><th></th></tr></thead><tbody id="rules"></tbody></table>
    <h2 style="margin-top:18px">版本历史</h2>
    <table><thead><tr><th>版本</th><th>时间</th><th>规则数</th><th>备注</th><th></th></tr></thead><tbody id="versions"></tbody></table>
  </section>
</main>
<script>
function j(url, method, body){
  return fetch(url, {method:method||'GET', headers:{'Content-Type':'application/json'},
    body: body?JSON.stringify(body):undefined}).then(function(r){return r.json().then(function(d){return {ok:r.ok,d:d}})});
}
function show(msg, good){
  var o=document.getElementById('out'); o.textContent=msg; o.style.color = good===undefined?'#e6e8ee':(good?'#3fb950':'#f85149');
}
function draft(){
  return {id:parseInt(document.getElementById('rid').value||'0',10),
    name:document.getElementById('rname').value,
    priority:parseInt(document.getElementById('rprio').value||'0',10),
    rule:document.getElementById('rexpr').value, enabled:true};
}
function testRule(){
  var row; try{row=JSON.parse(document.getElementById('rrow').value)}catch(e){return show('样本 JSON 解析失败: '+e,false)}
  j('/rules/test','POST',{rule:document.getElementById('rexpr').value, row:row}).then(function(r){
    if(!r.ok) return show('错误: '+(r.d.error||'unknown'),false);
    show(r.d.matched?'✓ 命中 (matched=true)':'✗ 未命中 (matched=false)', r.d.matched);
  });
}
function publishRule(){
  j('/rules','POST',draft()).then(function(r){
    if(!r.ok) return show('发布失败: '+(r.d.error||'unknown'),false);
    show('已发布规则 #'+draft().id+'，实时生效。',true); loadRules();
  });
}
function delRule(id){
  j('/rules/'+id,'DELETE').then(function(r){ show('已删除规则 #'+id,true); loadRules(); });
}
function snapshot(){
  var note=prompt('版本备注（可选）','')||'';
  j('/versions','POST',{note:note}).then(function(r){
    if(!r.ok) return show('快照失败',false);
    show('已创建版本 v'+r.d.version+'（'+r.d.count+' 条规则）',true); loadVersions();
  });
}
function rollback(v){
  j('/versions/'+v+'/rollback','POST').then(function(r){
    if(!r.ok) return show('回滚失败: '+(r.d.error||''),false);
    show('已回滚到版本 v'+v,true); loadRules();
  });
}
function esc(s){return (s+'').replace(/[&<>]/g,function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;'}[c]})}
function loadRules(){
  j('/rules/list').then(function(r){
    var rows=(r.d.rules||[]).map(function(x){
      return '<tr><td>'+x.id+'</td><td>'+esc(x.name)+'</td><td><code>'+esc(x.expr)+'</code></td>'+
        '<td><button class="danger" style="padding:3px 9px" onclick="delRule('+x.id+')">删除</button></td></tr>';
    }).join('');
    document.getElementById('rules').innerHTML = rows || '<tr><td colspan=4 class="mut">暂无规则</td></tr>';
  });
}
function loadVersions(){
  j('/versions').then(function(r){
    var rows=(r.d.versions||[]).map(function(v){
      return '<tr><td>v'+v.version+'</td><td class="mut">'+esc((v.time||'').replace('T',' ').slice(0,19))+'</td>'+
        '<td>'+v.count+'</td><td>'+esc(v.note||'')+'</td>'+
        '<td><button class="ghost" style="padding:3px 9px" onclick="rollback('+v.version+')">回滚</button></td></tr>';
    }).join('');
    document.getElementById('versions').innerHTML = rows || '<tr><td colspan=5 class="mut">暂无版本</td></tr>';
  });
}
loadRules(); loadVersions();
</script>
</body>
</html>`
