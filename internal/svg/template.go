package svg

const svgHTML = `
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {{.W}} {{.H}}" width="100%" font-family="ui-sans-serif,system-ui,sans-serif" font-size="11">
<rect x="0" y="0" width="{{.W}}" height="{{.H}}" fill="#ffffff"/>
<text x="{{.Left}}" y="20" font-size="14" font-weight="bold">TG / DTG / 炉程序 / 气氛时间线（同步时间轴）</text>
<text x="{{.Right}}" y="20" text-anchor="end">视图事件 seq={{.AtSeq}} / 头部 seq={{.HeadSeq}}</text>

<!-- gap bands behind everything -->
{{range .Gaps}}<rect data-id="{{.ID}}" x="{{.X0}}" y="40" width="{{.X1 | subtract .X0}}" height="610" fill="#fde68a" opacity="0.35"/>{{end}}
{{range .AtmBands}}<rect data-id="{{.ID}}" x="{{.X0}}" y="560" width="{{.X1 | subtract .X0}}" height="90" fill="#bfdbfe" opacity="0.55"/><text x="{{.X0}}" y="612" font-size="9">{{.Label}}</text>{{end}}

<!-- TG panel -->
<text x="{{$.Left}}" y="52" font-weight="bold">TG (mg)</text>
<rect x="{{$.Left}}" y="{{.TG.Top}}" width="{{subtract $.Right $.Left}}" height="{{.TG.H}}" fill="none" stroke="#94a3b8"/>
{{range .TG.StageBands}}<rect data-id="{{.ID}}" x="{{.X0}}" y="42" width="{{.X1 | subtract .X0}}" height="176" fill="#dcfce7" opacity="0.25"/><text x="{{.X0}}" y="54" font-size="9" fill="#166534">{{.Label}}</text>{{end}}
{{range .TG.Segments}}<polyline points="{{.}}" fill="none" stroke="#1d4ed8" stroke-width="1.6"/>{{end}}
{{range .TG.Bad}}<circle cx="{{.X}}" cy="{{.Y}}" r="3" fill="#dc2626"><title>{{.Title}}</title></circle>{{end}}
<line x1="{{$.Left}}" x2="{{$.Right}}" y1="{{tgzero .TG.Top .TG.H}}" y2="{{tgzero .TG.Top .TG.H}}" stroke="#e2e8f0"/>

<!-- DTG panel -->
<text x="{{$.Left}}" y="257" font-weight="bold">DTG (mg/s)</text>
<rect x="{{$.Left}}" y="{{.DTG.Top}}" width="{{subtract $.Right $.Left}}" height="{{.DTG.H}}" fill="none" stroke="#94a3b8"/>
<line x1="{{$.Left}}" x2="{{$.Right}}" y1="{{dtgzero .DTG.Top .DTG.H}}" y2="{{dtgzero .DTG.Top .DTG.H}}" stroke="#cbd5e1" stroke-dasharray="3 3"/>
{{range .DTG.Segments}}<polyline points="{{.}}" fill="none" stroke="#b91c1c" stroke-width="1.4"/>{{end}}
{{range .DTG.Peaks}}<circle data-id="{{.ID}}" cx="{{.X}}" cy="{{.Y}}" r="3.5" fill="#f59e0b" stroke="#92400e"><title>{{.Label}}</title></circle><text x="{{.X}}" y="{{subtract .Y 6}}" text-anchor="middle" font-size="9">{{.Label}}</text>{{end}}

<!-- furnace panel -->
<text x="{{$.Left}}" y="432" font-weight="bold">炉温 (°C)</text>
<rect x="{{$.Left}}" y="{{.Furnace.Top}}" width="{{subtract $.Right $.Left}}" height="{{.Furnace.H}}" fill="none" stroke="#94a3b8"/>
<polyline points="{{.Furnace.Polyline}}" fill="none" stroke="#0f766e" stroke-width="1.6"/>

<!-- atmosphere panel -->
<text x="{{$.Left}}" y="572" font-weight="bold">气氛</text>
<rect x="{{$.Left}}" y="{{.Atmosphere.Top}}" width="{{subtract $.Right $.Left}}" height="{{.Atmosphere.H}}" fill="#f8fafc" stroke="#94a3b8"/>
{{range .Atmosphere.Valves}}<line data-id="{{.ID}}" x1="{{.X}}" x2="{{.X}}" y1="{{subtract .Y 18}}" y2="{{add .Y 18}}" stroke="#1e40af" stroke-width="2"/><text x="{{.X}}" y="{{subtract .Y 22}}" text-anchor="middle" font-size="9">{{.Label}}</text>{{end}}

<!-- shared time axis -->
<line x1="{{$.Left}}" x2="{{$.Right}}" y1="665" y2="665" stroke="#64748b"/>
{{range .XTicks}}<line x1="{{.X}}" x2="{{.X}}" y1="665" y2="670" stroke="#64748b"/><text x="{{.X}}" y="682" text-anchor="middle">{{.Label}}</text>{{end}}

<text x="{{$.Left}}" y="710" fill="#475569">蓝色阴影=阶段候选(首选组合)；黄色=采样缺口(不插值)；蓝色带=气氛反应窗口(阀时间+延迟±半宽)。点击右侧工件可定位。</text>
</svg>`
