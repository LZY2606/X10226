package httpapi

const pageHTML = `<!doctype html>
<html lang="zh">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>TGA 阶段分解 / 气氛事件证据库</title>
<link rel="stylesheet" href="/static/app.css"/>
</head>
<body>
<header>
  <h1>热重分析证据库</h1>
  <span class="sub">TG/DTG · 阶段分解 · 气氛事件 · 残留物计算链 · 只追加事件历史</span>
  <span style="flex:1"></span>
  <label class="pill">导入归档<input type="file" data-act="import-archive" accept="application/json" style="display:none"/></label>
</header>
<main>
  <div class="card">
    <h2>新建项目（热分析团队导入质量、温度、炉时间与气氛事件）</h2>
    <form onsubmit="event.preventDefault();TGA.createProject(this)">
      <div class="row">
        <input name="name" placeholder="项目名称，例如 CaCO3-2026-09 空气/N2" style="flex:2"/>
        <input name="caseId" placeholder="案例 id（可空）" style="flex:1"/>
        <button type="submit">创建并进入审阅</button>
      </div>
    </form>
    <p class="muted" id="flash"></p>
  </div>

  <div class="card">
    <h2>离线领域案例包</h2>
    <p class="muted">失重谱系：TGA 阶段分解、气氛切换与残留物证据链。实例化即导入六层基线材料（固定 seed，无需公网）。</p>
    {{range .Cases}}
    <div class="row spread" style="margin:8px 0">
      <div>
        <b>{{.Title}}</b><br/>
        <span class="muted">id={{.ID}} · seed={{.Seed}} · {{len .Files}} 个材料文件 · {{len .Replay}} 个重放事件</span>
      </div>
      <button data-act="instantiate" data-cid="{{.ID}}">实例化并导入基线</button>
    </div>
    {{else}}<p class="muted">未发现嵌入案例包。</p>{{end}}
  </div>

  <div class="card">
    <h2>已有项目</h2>
    <table>
      <tr><th>项目</th><th>案例</th><th>创建时间（虚拟/真实）</th><th></th></tr>
      {{range .Projects}}
      <tr>
        <td>{{.Name}}<br/><span class="id">{{.ID}}</span></td>
        <td>{{if .CaseID}}{{.CaseID}}{{else}}—{{end}}</td>
        <td class="muted">{{.CreatedAt}}</td>
        <td><a href="/projects/{{.ID}}">打开复核 →</a></td>
      </tr>
      {{else}}<tr><td colspan="4" class="muted">还没有项目。</td></tr>{{end}}
    </table>
  </div>
</main>
<script src="/static/app.js"></script>
</body></html>`
