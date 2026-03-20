package monitor

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// StatsProvider provides usage statistics for the dashboard.
type StatsProvider interface {
	QueryDailyJSON(days int) (daily any, totalUsers int, totalDownloads int, err error)
}

// RegisterRoutes registers the monitoring dashboard and API endpoints.
// activeTasksFn is an optional callback returning (active, capacity) for task monitoring.
func RegisterRoutes(r gin.IRouter, m *Metrics, adminToken string, stats StatsProvider, activeTasksFn func() (int, int)) {
	// Health check is always public (used by Docker HEALTHCHECK)
	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	group := r.Group("")
	if adminToken != "" {
		group.Use(func(c *gin.Context) {
			auth := c.GetHeader("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != adminToken {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				return
			}
			c.Next()
		})
	}

	group.GET("/api/metrics", func(c *gin.Context) {
		snap := m.Snapshot()
		if activeTasksFn != nil {
			snap.ActiveTasks, snap.TaskCapacity = activeTasksFn()
		}
		c.JSON(http.StatusOK, snap)
	})

	group.GET("/api/stats", func(c *gin.Context) {
		if stats == nil {
			c.JSON(http.StatusOK, gin.H{"daily": nil, "total_users": 0, "total_downloads": 0})
			return
		}
		daily, totalUsers, totalDL, err := stats.QueryDailyJSON(30)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"daily": nil, "total_users": 0, "total_downloads": 0})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"daily":           daily,
			"total_users":     totalUsers,
			"total_downloads": totalDL,
		})
	})

	group.GET("/dashboard", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(dashboardHTML))
	})
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Sticker Bot - Dashboard</title>
<script src="https://cdn.tailwindcss.com"></script>
<style>
  body { background: #0f172a; color: #e2e8f0; }
  .card { background: #1e293b; border-radius: 0.75rem; padding: 1.25rem; }
  .card-title { font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; color: #94a3b8; margin-bottom: 0.25rem; }
  .card-value { font-size: 1.75rem; font-weight: 700; color: #f1f5f9; }
  table { width: 100%; border-collapse: collapse; }
  th { text-align: left; font-size: 0.75rem; text-transform: uppercase; color: #94a3b8; padding: 0.5rem; border-bottom: 1px solid #334155; }
  td { padding: 0.5rem; border-bottom: 1px solid #1e293b; font-size: 0.875rem; }
  .bar-cell { position: relative; min-width: 100px; }
  .bar-cell .bar-bg { position: absolute; left: 0; top: 50%; transform: translateY(-50%); height: 20px; border-radius: 3px; opacity: 0.25; transition: width 0.3s ease; }
  .bar-cell .bar-val { position: relative; z-index: 1; text-align: right; }
  .bar-users { background: #3b82f6; }
  .bar-single { background: #22c55e; }
  .bar-set { background: #a855f7; }
  .bar-info { background: #f59e0b; }
</style>
</head>
<body class="min-h-screen p-6">
<div class="max-w-4xl mx-auto">
  <h1 class="text-2xl font-bold mb-6 text-slate-200">Sticker Bot Monitor</h1>

  <div class="grid grid-cols-3 gap-4 mb-4">
    <div class="card">
      <div class="card-title">Uptime</div>
      <div class="card-value" id="uptime">-</div>
    </div>
    <div class="card">
      <div class="card-title">Memory</div>
      <div class="card-value" id="memory">-</div>
    </div>
    <div class="card">
      <div class="card-title">Goroutines</div>
      <div class="card-value" id="goroutines">-</div>
    </div>
  </div>

  <div class="grid grid-cols-4 gap-4 mb-4">
    <div class="card">
      <div class="card-title">Requests / min</div>
      <div class="card-value" id="rpm">-</div>
      <div class="text-xs text-slate-500 mt-1">Total: <span id="total">-</span></div>
    </div>
    <div class="card">
      <div class="card-title">Error Rate</div>
      <div class="card-value" id="errorRate">-</div>
      <div class="text-xs text-slate-500 mt-1">Errors: <span id="errors">-</span></div>
    </div>
    <div class="card">
      <div class="card-title">Rate Limited</div>
      <div class="card-value" id="rateDenied">-</div>
    </div>
    <div class="card">
      <div class="card-title">Active Tasks</div>
      <div class="card-value"><span id="activeTasks">-</span> <span class="text-base font-normal text-slate-400">/ <span id="taskCapacity">-</span></span></div>
    </div>
  </div>

  <div class="card">
    <div class="card-title mb-2">Command Stats</div>
    <table>
      <thead><tr><th>Command</th><th class="text-right">Count</th></tr></thead>
      <tbody id="cmdTable"></tbody>
    </table>
  </div>

  <div class="grid grid-cols-2 gap-4 mt-4 mb-4">
    <div class="card">
      <div class="card-title">Total Users (anonymized)</div>
      <div class="card-value" id="totalUsers">-</div>
    </div>
    <div class="card">
      <div class="card-title">Total Downloads</div>
      <div class="card-value" id="totalDL">-</div>
    </div>
  </div>

  <div class="card mt-4">
    <div class="card-title mb-2">Daily Stats (last 30 days)</div>
    <table>
      <thead><tr><th>Date</th><th class="text-right">Users</th><th class="text-right">Single DL</th><th class="text-right">Set DL</th><th class="text-right">Info</th></tr></thead>
      <tbody id="dailyTable"></tbody>
    </table>
  </div>
</div>

<script>
function update() {
  fetch('/api/metrics')
    .then(r => r.json())
    .then(d => {
      document.getElementById('uptime').textContent = d.uptime;
      document.getElementById('memory').textContent = d.memory_mb.toFixed(1) + ' MB';
      document.getElementById('goroutines').textContent = d.goroutines;
      document.getElementById('rpm').textContent = d.requests_per_min;
      document.getElementById('total').textContent = d.total_requests;
      document.getElementById('errorRate').textContent = d.error_rate.toFixed(2) + '%';
      document.getElementById('errors').textContent = d.error_count;
      document.getElementById('rateDenied').textContent = d.rate_denied;
      document.getElementById('activeTasks').textContent = d.active_tasks;
      document.getElementById('taskCapacity').textContent = d.task_capacity;

      var tb = document.getElementById('cmdTable');
      tb.innerHTML = '';
      Object.entries(d.command_counts || {}).sort((a,b) => b[1]-a[1]).forEach(function(e) {
        tb.innerHTML += '<tr><td>' + e[0] + '</td><td class="text-right">' + e[1] + '</td></tr>';
      });
    })
    .catch(function(err) { console.error('fetch error', err); });

  fetch('/api/stats')
    .then(r => r.json())
    .then(d => {
      document.getElementById('totalUsers').textContent = d.total_users || 0;
      document.getElementById('totalDL').textContent = d.total_downloads || 0;

      var dt = document.getElementById('dailyTable');
      dt.innerHTML = '';
      var rows = d.daily || [];
      var maxUsers = 0, maxSingle = 0, maxSet = 0, maxInfo = 0;
      rows.forEach(function(r) {
        if (r.unique_users > maxUsers) maxUsers = r.unique_users;
        if (r.single_downloads > maxSingle) maxSingle = r.single_downloads;
        if (r.set_downloads > maxSet) maxSet = r.set_downloads;
        if (r.info_queries > maxInfo) maxInfo = r.info_queries;
      });
      function barCell(val, max, cls) {
        var pct = max > 0 ? (val / max * 100) : 0;
        return '<td class="bar-cell"><div class="bar-bg ' + cls + '" style="width:' + pct + '%"></div><div class="bar-val">' + val + '</div></td>';
      }
      rows.forEach(function(r) {
        dt.innerHTML += '<tr><td>' + r.date + '</td>' +
          barCell(r.unique_users, maxUsers, 'bar-users') +
          barCell(r.single_downloads, maxSingle, 'bar-single') +
          barCell(r.set_downloads, maxSet, 'bar-set') +
          barCell(r.info_queries, maxInfo, 'bar-info') + '</tr>';
      });
    })
    .catch(function(err) { console.error('stats error', err); });
}
update();
setInterval(update, 5000);
</script>
</body>
</html>`
