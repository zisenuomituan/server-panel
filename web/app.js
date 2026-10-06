const { createApp } = Vue;

const emptyLive = () => ({
  server_id: 0, host_id: 0, name: '', state: '',
  cpu: 0, mem_used_mb: 0, mem_total_mb: 0, disk_used: 0, disk_total: 0,
  net_rx_rate: 0, net_tx_rate: 0, net_rx: 0, net_tx: 0, uptime: 0, updated: null,
});

createApp({
  data() {
    return {
      token: localStorage.getItem('token') || '',
      user: JSON.parse(localStorage.getItem('user') || 'null') || { username: '', role: '' },
      tab: 'login',
      form: { username: '', password: '', email: '', invite: '', bindKey: '' },
      inviteRequired: false,
      error: '',
      busy: false,

      hosts: [],
      metrics: {},
      selected: null,
      history: [],

      bindInput: '',
      view: 'main',
      pwdOpen: false,
      pwd: { old: '', new: '' },
      logs: [],
      keyGroups: [],
      users: [],
      newUser: { username: '', password: '', role: 'operator', email: '' },
      execEnabled: true,
      settings: { enable_exec: true },
      cmd: '',
      cmdBusy: false,
      termOut: '',
      cmdHistory: [],
      histIdx: 0,
      newHost: { name: '', ssh_host: '', ssh_port: '', ssh_user: '', libvirt_uri: '' },
      newKey: '',

      toast: '',
      toastErr: false,
      ws: null,
      charts: {},
      appTipClosed: localStorage.getItem('appTipClosed') === '1',
    };
  },

  computed: {
    roleText() {
      return { admin: '管理员', operator: '操作员', viewer: '只读' }[this.user.role] || this.user.role;
    },
  },

  mounted() {
    if (this.token) {
      this.afterLogin();
    }
  },

  methods: {
    // ---------- 下载 App ----------
    downloadApp() {
      window.location.href = '/android/download';
    },

    closeAppTip() {
      this.appTipClosed = true;
      localStorage.setItem('appTipClosed', '1');
    },

    // ---------- 网络 ----------

    async api(path, opts = {}) {
      const headers = Object.assign({ 'Content-Type': 'application/json' }, opts.headers || {});
      if (this.token) headers.Authorization = 'Bearer ' + this.token;
      const res = await fetch('/api' + path, Object.assign({}, opts, { headers }));
      const text = await res.text();
      let body = {};
      if (text) { try { body = JSON.parse(text); } catch { body = {}; } }
      if (!res.ok) throw new Error(body.error || ('请求失败 ' + res.status));
      return body;
    },

    // ---------- 认证 ----------
    async login() {
      this.error = '';
      this.busy = true;
      try {
        const r = await this.api('/auth/login', {
          method: 'POST',
          body: JSON.stringify({
            username: this.form.username,
            password: this.form.password,
            bind_key: this.form.bindKey,
          }),
        });
        this.setSession(r.token, r.user);
        this.afterLogin();
        if (r.bound) this.notify(r.bound);
      } catch (e) {
        this.error = e.message;
      } finally {
        this.busy = false;
      }
    },

    async register() {
      this.error = '';
      this.busy = true;
      try {
        const r = await this.api('/auth/register', {
          method: 'POST',
          body: JSON.stringify(this.form),
        });
        this.setSession(r.token, r.user);
        this.afterLogin();
      } catch (e) {
        this.error = e.message;
      } finally {
        this.busy = false;
      }
    },

    setSession(token, user) {
      this.token = token;
      this.user = user;
      localStorage.setItem('token', token);
      localStorage.setItem('user', JSON.stringify(user));
    },

    logout() {
      if (this.ws) this.ws.close();
      this.ws = null;
      this.token = '';
      this.user = { username: '', role: '' };
      this.hosts = [];
      this.metrics = {};
      this.selected = null;
      this.view = 'main';
      this.logs = [];
      localStorage.removeItem('token');
      localStorage.removeItem('user');
    },

    afterLogin() {
      this.loadHosts();
      this.loadConfig();
      this.connectWS();
    },

    async loadConfig() {
      try {
        const c = await this.api('/config');
        this.execEnabled = !!c.enable_exec;
        this.settings.enable_exec = this.execEnabled;
      } catch {}
    },

    async loadSettings() {
      try {
        const s = await this.api('/admin/settings');
        this.settings.enable_exec = !!s.enable_exec;
        this.execEnabled = this.settings.enable_exec;
      } catch (e) {
        this.notify(e.message, true);
      }
    },

    async saveSettings() {
      try {
        const s = await this.api('/admin/settings', {
          method: 'POST',
          body: JSON.stringify({ enable_exec: this.settings.enable_exec }),
        });
        this.execEnabled = !!s.enable_exec;
        this.notify('设置已保存');
      } catch (e) {
        this.notify(e.message, true);
        this.loadSettings();
      }
    },

    // ---------- 数据 ----------
    async loadHosts() {
      try {
        this.hosts = await this.api('/hosts');
        this.hosts.forEach((h) => {
          (h.servers || []).forEach((sv) => {
            if (sv.live) this.metrics[sv.id] = sv.live;
          });
        });
      } catch (e) {
        this.notify(e.message, true);
      }
    },

    connectWS() {
      if (this.ws) this.ws.close();
      const proto = location.protocol === 'https:' ? 'wss' : 'ws';
      const ws = new WebSocket(`${proto}://${location.host}/api/ws?token=${encodeURIComponent(this.token)}`);
      ws.onmessage = (ev) => {
        const msg = JSON.parse(ev.data);
        if (msg.type === 'metric') {
          this.metrics[msg.data.server_id] = msg.data;
          if (this.selected && msg.data.server_id === this.selected.server.id) {
            this.pushPoint(msg.data);
          }
        }
      };
      ws.onclose = () => { this.ws = null; };
      this.ws = ws;
    },

    async bind() {
      if (!this.bindInput) return;
      this.busy = true;
      try {
        const r = await this.api('/bind', { method: 'POST', body: JSON.stringify({ key: this.bindInput }) });
        this.bindInput = '';
        this.notify(r.message || '已绑定');
        this.loadHosts();
      } catch (e) {
        this.notify(e.message, true);
      } finally {
        this.busy = false;
      }
    },

    async unbind(hostID) {
      if (!confirm('确定解绑这台宿主机？')) return;
      try {
        await this.api(`/hosts/${hostID}/unbind`, { method: 'POST' });
        this.loadHosts();
      } catch (e) {
        this.notify(e.message, true);
      }
    },

    async openServer(id) {
      try {
        this.selected = await this.api('/servers/' + id);
        this.termOut = '';
        this.cmd = '';
        this.cmdHistory = [];
        this.histIdx = 0;
        await this.loadHistory(id);
      } catch (e) {
        this.notify(e.message, true);
      }
    },

    async changePassword() {
      try {
        await this.api('/me/password', {
          method: 'POST',
          body: JSON.stringify({ old: this.pwd.old, new: this.pwd.new }),
        });
        this.pwd = { old: '', new: '' };
        this.pwdOpen = false;
        this.notify('密码已修改');
      } catch (e) {
        this.notify(e.message, true);
      }
    },

    toggleView(v) {
      this.view = this.view === v ? 'main' : v;
      if (this.view === 'admin') {
        this.loadKeys();
        this.loadUsers();
        this.loadSettings();
      }
      if (this.view === 'logs') this.loadLogs();
      if (this.view === 'main' && this.selected) {
        this.$nextTick(() => this.drawCharts());
      }
    },

    async loadLogs() {
      try {
        this.logs = await this.api('/audit');
      } catch (e) {
        this.notify(e.message, true);
      }
    },

    // ---------- 命令行 ----------
    async runCmd() {
      const command = this.cmd.trim();
      if (!command || this.cmdBusy || !this.selected) return;
      this.cmdBusy = true;
      this.cmdHistory.push(command);
      this.histIdx = this.cmdHistory.length;
      this.cmd = '';
      this.termOut += `$ ${command}\n`;
      try {
        const r = await this.api(`/servers/${this.selected.server.id}/exec`, {
          method: 'POST',
          body: JSON.stringify({ command }),
        });
        this.termOut += r.output || '';
        if (this.termOut && !this.termOut.endsWith('\n')) this.termOut += '\n';
        if (r.exit_code !== 0) this.termOut += `[退出码 ${r.exit_code}]\n`;
      } catch (e) {
        this.termOut += `错误: ${e.message}\n`;
      } finally {
        this.cmdBusy = false;
        this.$nextTick(this.scrollTerm);
      }
    },

    histPrev() {
      if (!this.cmdHistory.length) return;
      this.histIdx = Math.max(0, this.histIdx - 1);
      this.cmd = this.cmdHistory[this.histIdx] || '';
    },

    histNext() {
      if (!this.cmdHistory.length) return;
      this.histIdx = Math.min(this.cmdHistory.length, this.histIdx + 1);
      this.cmd = this.cmdHistory[this.histIdx] || '';
    },

    scrollTerm() {
      const el = this.$refs.term;
      if (el) el.scrollTop = el.scrollHeight;
    },

    closeServer() {
      this.selected = null;
      this.disposeCharts();
    },

    async loadHistory(id) {
      try {
        const pts = await this.api(`/servers/${id}/history?limit=120`);
        this.history = Array.isArray(pts) ? pts.reverse() : [];
      } catch {
        this.history = [];
      }
      this.$nextTick(() => this.drawCharts());
    },

    async power(action) {
      const names = { start: '开机', shutdown: '关机', 'force-off': '强制关机', reboot: '重启' };
      const id = this.selected.server.id;
      if (!confirm(`确认对 ${this.selected.server.name} 执行「${names[action]}」？`)) return;
      this.busy = true;
      try {
        await this.api(`/servers/${id}/power`, {
          method: 'POST',
          body: JSON.stringify({ action }),
        });
        this.notify('指令已下发');
        // 后台已触发一次采集，稍等再拉一次就能看到新状态
        setTimeout(async () => {
          try {
            const fresh = await this.api('/servers/' + id);
            if (this.selected && this.selected.server.id === id) this.selected = fresh;
          } catch {}
          this.loadHosts();
        }, 900);
      } catch (e) {
        this.notify(e.message, true);
      } finally {
        this.busy = false;
      }
    },

    // ---------- 管理 ----------
    async createHost() {
      try {
        const r = await this.api('/admin/hosts', { method: 'POST', body: JSON.stringify(this.newHost) });
        this.newKey = r.formatted;
        this.newHost = { name: '', ssh_host: '', ssh_port: '', ssh_user: '', libvirt_uri: '' };
        this.loadKeys();
      } catch (e) {
        this.notify(e.message, true);
      }
    },

    async loadKeys() {
      try {
        this.keyGroups = await this.api('/admin/keys');
      } catch (e) {
        this.notify(e.message, true);
      }
    },

    async rotateKey(hostID) {
      if (!confirm('轮换后旧密钥立即失效，确定继续？')) return;
      try {
        const r = await this.api(`/admin/hosts/${hostID}/rotate`, { method: 'POST', body: '{}' });
        this.newKey = r.formatted;
        this.loadKeys();
      } catch (e) {
        this.notify(e.message, true);
      }
    },

    async deleteHost(hostID) {
      if (!confirm('删除宿主会一并移除它的虚拟机、指标和绑定，确定？')) return;
      try {
        await this.api(`/admin/hosts/${hostID}`, { method: 'DELETE' });
        this.notify('已删除');
        this.loadKeys();
        this.loadHosts();
      } catch (e) {
        this.notify(e.message, true);
      }
    },

    async loadUsers() {
      try {
        this.users = await this.api('/admin/users');
      } catch (e) {
        this.notify(e.message, true);
      }
    },

    async createUser() {
      try {
        await this.api('/admin/users', { method: 'POST', body: JSON.stringify(this.newUser) });
        this.newUser = { username: '', password: '', role: 'operator', email: '' };
        this.notify('用户已创建');
        this.loadUsers();
      } catch (e) {
        this.notify(e.message, true);
      }
    },

    async setRole(u, role) {
      if (role === u.role) return;
      try {
        await this.api(`/admin/users/${u.id}`, { method: 'PATCH', body: JSON.stringify({ role }) });
        this.notify(`${u.username} 角色改为 ${this.roleName(role)}`);
        this.loadUsers();
      } catch (e) {
        this.notify(e.message, true);
        this.loadUsers();
      }
    },

    async resetUserPassword(id) {
      const pw = prompt('输入新密码（至少 8 位）');
      if (!pw) return;
      try {
        await this.api(`/admin/users/${id}/password`, { method: 'POST', body: JSON.stringify({ password: pw }) });
        this.notify('密码已重置');
      } catch (e) {
        this.notify(e.message, true);
      }
    },

    async deleteUser(u) {
      if (!confirm(`确定删除用户 ${u.username}？`)) return;
      try {
        await this.api(`/admin/users/${u.id}`, { method: 'DELETE' });
        this.notify('已删除');
        this.loadUsers();
      } catch (e) {
        this.notify(e.message, true);
      }
    },

    roleName(r) {
      return { admin: '管理员', operator: '操作员', viewer: '只读' }[r] || r;
    },

    async rotateServerKey(serverID) {
      if (!confirm('轮换后该虚拟机旧的单机密钥立即失效，确定继续？')) return;
      try {
        const r = await this.api(`/admin/servers/${serverID}/rotate`, { method: 'POST', body: '{}' });
        this.newKey = r.formatted;
        this.loadKeys();
      } catch (e) {
        this.notify(e.message, true);
      }
    },

    async revokeKey(id) {
      try {
        await this.api(`/admin/keys/${id}/revoke`, { method: 'POST', body: '{}' });
        this.loadKeys();
      } catch (e) {
        this.notify(e.message, true);
      }
    },

    // ---------- 图表 ----------
    pushPoint(m) {
      if (!this.selected || m.server_id !== this.selected.server.id) return;
      this.history.push({
        ts: m.updated,
        cpu: m.cpu,
        mem_used_mb: m.mem_used_mb,
        mem_total_mb: m.mem_total_mb,
        net_rx_rate: m.net_rx_rate,
        net_tx_rate: m.net_tx_rate,
      });
      if (this.history.length > 150) this.history.splice(0, this.history.length - 150);
      this.drawCharts();
    },

    drawCharts() {
      const res = document.getElementById('chart-res');
      const net = document.getElementById('chart-net');
      if (!res || !net) return;

      const times = this.history.map((p) => new Date(p.ts).toLocaleTimeString('zh-CN', { hour12: false }));
      const cpu = this.history.map((p) => p.cpu);
      const mem = this.history.map((p) => (p.mem_total_mb ? (p.mem_used_mb / p.mem_total_mb) * 100 : 0));
      const rx = this.history.map((p) => p.net_rx_rate);
      const tx = this.history.map((p) => p.net_tx_rate);

      const base = {
        grid: { left: 48, right: 16, top: 24, bottom: 28 },
        tooltip: { trigger: 'axis' },
        xAxis: { type: 'category', data: times, axisLine: { lineStyle: { color: '#262d35' } }, axisLabel: { color: '#7d8894' } },
        yAxis: { type: 'value', max: 100, splitLine: { lineStyle: { color: '#1d2329' } }, axisLabel: { color: '#7d8894', formatter: '{value}%' } },
      };

      this.charts.res = (this.charts.res || echarts.init(res));
      this.charts.res.setOption({
        ...base,
        legend: { data: ['CPU', '内存'], textStyle: { color: '#7d8894' }, right: 10 },
        series: [
          { name: 'CPU', type: 'line', smooth: true, showSymbol: false, data: cpu, lineStyle: { color: '#6aa6f0' }, areaStyle: { color: 'rgba(106,166,240,.08)' } },
          { name: '内存', type: 'line', smooth: true, showSymbol: false, data: mem, lineStyle: { color: '#57c08a' } },
        ],
      });

      this.charts.net = (this.charts.net || echarts.init(net));
      this.charts.net.setOption({
        ...base,
        yAxis: { type: 'value', splitLine: { lineStyle: { color: '#1d2329' } }, axisLabel: { color: '#7d8894', formatter: (v) => this.rate(v) } },
        legend: { data: ['下行', '上行'], textStyle: { color: '#7d8894' }, right: 10 },
        series: [
          { name: '下行', type: 'line', smooth: true, showSymbol: false, data: rx, lineStyle: { color: '#6aa6f0' } },
          { name: '上行', type: 'line', smooth: true, showSymbol: false, data: tx, lineStyle: { color: '#d7a44b' } },
        ],
      });
    },

    disposeCharts() {
      Object.values(this.charts).forEach((c) => c && c.dispose());
      this.charts = {};
    },

    // ---------- 展示辅助 ----------
    metricFor(id) {
      return this.metrics[id] || emptyLive();
    },
    liveOf(id) {
      return this.metricFor(id);
    },
    running(live) {
      return live && live.state === 'running';
    },
    stateText(live) {
      if (!live || !live.state) return '未知';
      return { running: '运行中', paused: '重启中', 'shut off': '已停止' }[live.state] || '未知';
    },
    badgeClass(live) {
      if (!live || !live.state) return 'shut';
      return { running: 'running', paused: 'warn' }[live.state] || 'shut';
    },
    dotClass(live) {
      if (!live || !live.state) return 'unknown';
      return { running: 'on', paused: 'warn' }[live.state] || 'off';
    },
    clamp(v) {
      return Math.max(0, Math.min(100, v || 0));
    },
    pct(v) {
      return (v || 0).toFixed(1);
    },
    barClass(v) {
      if (v >= 85) return 'crit';
      if (v >= 65) return 'warn';
      return '';
    },
    memPct(l) {
      return l.mem_total_mb ? Math.min(100, (l.mem_used_mb / l.mem_total_mb) * 100) : 0;
    },
    diskPct(l) {
      return l.disk_total ? Math.min(100, (l.disk_used / l.disk_total) * 100) : 0;
    },
    memText(l) {
      if (!l.mem_total_mb) return '-';
      return `${this.bytes(l.mem_used_mb * 1048576)} / ${this.bytes(l.mem_total_mb * 1048576)}`;
    },
    diskText(l) {
      if (!l.disk_total) return '-';
      return `${this.bytes(l.disk_used)} / ${this.bytes(l.disk_total)}`;
    },
    bytes(n) {
      if (!n) return '0';
      const u = ['B', 'K', 'M', 'G', 'T'];
      let i = 0;
      while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
      return n.toFixed(n >= 10 || i === 0 ? 0 : 1) + u[i];
    },
    rate(n) {
      return this.bytes(n) + '/s';
    },
    uptime(sec) {
      if (!sec) return '-';
      const d = Math.floor(sec / 86400);
      const h = Math.floor((sec % 86400) / 3600);
      const m = Math.floor((sec % 3600) / 60);
      if (d) return `${d}天${h}时`;
      if (h) return `${h}时${m}分`;
      return `${m}分`;
    },
    time(t) {
      if (!t) return '-';
      return new Date(t).toLocaleTimeString('zh-CN', { hour12: false });
    },
    date(t) {
      return new Date(t).toLocaleDateString('zh-CN');
    },
    dateTime(t) {
      if (!t) return '-';
      return new Date(t).toLocaleString('zh-CN', { hour12: false });
    },
    actionText(a) {
      return {
        login: '登录', register: '注册', bind: '绑定', unbind: '解绑',
        create_host: '登记宿主机', delete_host: '删除宿主', rotate_key: '轮换密钥', revoke_key: '撤销密钥',
        user_create: '新建用户', user_role: '改角色', user_delete: '删除用户', user_passwd: '重置密码',
        power: '电源', exec: '执行命令', host_exec: '宿主命令', change_password: '改密',
      }[a] || a;
    },
    targetName(l) {
      if (l.server_id) {
        for (const h of this.hosts) {
          const sv = (h.servers || []).find((s) => s.id === l.server_id);
          if (sv) return sv.name;
        }
        return 'VM#' + l.server_id;
      }
      if (l.host_id) {
        const h = this.hosts.find((x) => x.id === l.host_id);
        return h ? h.name : '宿主#' + l.host_id;
      }
      return '-';
    },
    isBad(result) {
      return !!result && result.indexOf('失败') === 0;
    },
    notify(msg, isErr) {
      this.toast = msg;
      this.toastErr = !!isErr;
      clearTimeout(this._toastTimer);
      this._toastTimer = setTimeout(() => { this.toast = ''; }, 2600);
    },
  },

}).mount('#app');
