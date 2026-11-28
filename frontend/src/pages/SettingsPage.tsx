import { useEffect, useReducer, useState } from 'react';
import { api } from '../api/client';
import { Button, Card, ConfirmDialog, Input, Select, Switch } from '../components/ui';
import { formatBytes } from '../lib/format';
import { useAuthStore } from '../stores/useAuthStore';
import { useLiveStore } from '../stores/useLiveStore';
import { usePrefsStore } from '../stores/usePrefsStore';
import { useToastStore } from '../stores/useToastStore';
import type { DBStats, MihomoConfig, Preferences } from '../types/api';
const fields: [string, string, string[]?][] = [['mode', '运行模式', ['rule', 'global', 'direct']], ['bind-address', '绑定地址'], ['mixed-port', '混合端口'], ['port', 'HTTP 端口'], ['socks-port', 'SOCKS 端口'], ['redir-port', '重定向端口'], ['tproxy-port', 'TProxy 端口'], ['log-level', '日志级别', ['silent', 'error', 'warning', 'info', 'debug']], ['find-process-mode', '进程匹配', ['always', 'strict', 'off']], ['geo-update-interval', 'Geo 更新间隔（小时）'], ['global-ua', '全局 User-Agent']];
const switches: [string, string][] = [['allow-lan', '允许局域网'], ['ipv6', 'IPv6'], ['sniffing', '嗅探'], ['tcp-concurrent', 'TCP 并发'], ['unified-delay', '统一延迟'], ['geodata-mode', 'GeoData 模式'], ['geo-auto-update', '自动更新 Geo']];
const portFields = ['mixed-port', 'port', 'socks-port', 'redir-port', 'tproxy-port'];
const isNumericConfigField = (key: string) => portFields.includes(key) || key === 'geo-update-interval';
const optionLabels: Record<string, string> = {
  rule: '规则', global: '全局', direct: '直连', silent: '静默', error: '错误',
  warning: '警告', info: '信息', debug: '调试', always: '始终', strict: '严格',
  off: '关闭', system: '系统', mixed: '混合', normal: '普通', light: '浅色',
  dark: '深色', default: '默认', delay: '延迟', name: '名称', active: '实时', history: '历史',
};
const emptyDraft = { delayTestUrl: '', delayTestTimeout: '', retentionDays: '', snapshotIntervalMin: '', infoNodePattern: '' };
type PrefsDraft = typeof emptyDraft;
type Confirmation =
  | { kind: 'config'; changes: Partial<MihomoConfig> }
  | { kind: 'reload' | 'restart' | 'connections' | 'logs' };

export default function SettingsPage() {
  const [original, setOriginal] = useState<MihomoConfig | null>(null);
  const [configEdits, setConfigEdits] = useState<Partial<MihomoConfig>>({});
  const [tunEdits, setTunEdits] = useState<Record<string, unknown>>({});
  const [savingConfig, setSavingConfig] = useState(false);
  const [configRevision, refreshConfig] = useReducer(n => n + 1, 0);
  const [stats, setStats] = useState<DBStats | null>(null);
  const [statsRevision, refreshStats] = useReducer(n => n + 1, 0);
  const [domain, setDomain] = useState('');
  const [dnsType, setDnsType] = useState('A');
  const [dnsResult, setDnsResult] = useState('');
  const [confirm, setConfirm] = useState<Confirmation | null>(null);
  const [current, setCurrent] = useState('');
  const [password, setPassword] = useState('');
  const [repeat, setRepeat] = useState('');
  const [draftEdits, setDraftEdits] = useState<Partial<PrefsDraft>>({});
  const [savingDraft, setSavingDraft] = useState(false);
  const prefs = usePrefsStore(s => s.prefs);
  const savePrefs = usePrefsStore(s => s.savePrefs);
  const username = useAuthStore(s => s.user?.username);
  const updatePassword = useAuthStore(s => s.updatePassword);
  const logout = useAuthStore(s => s.logout);
  const status = useLiveStore(s => s.status);
  const showError = useToastStore(s => s.error);
  const showSuccess = useToastStore(s => s.success);

  const savedDraft = prefs ? {
    delayTestUrl: prefs.delayTestUrl,
    delayTestTimeout: String(prefs.delayTestTimeout),
    retentionDays: String(prefs.retentionDays),
    snapshotIntervalMin: String(prefs.snapshotIntervalMin),
    infoNodePattern: prefs.infoNodePattern,
  } : emptyDraft;
  const draft = { ...savedDraft, ...draftEdits };
  const dirty = Object.entries(draftEdits).some(([key, value]) => value !== savedDraft[key as keyof PrefsDraft]);
  const editDraft = (key: keyof PrefsDraft, value: string) => setDraftEdits(edits => {
    const next = { ...edits };
    if (value === savedDraft[key]) delete next[key];
    else next[key] = value;
    return next;
  });
  const saveDraft = () => {
    const values = [draft.delayTestTimeout, draft.retentionDays, draft.snapshotIntervalMin].map(value => value.trim());
    const [delayTestTimeout, retentionDays, snapshotIntervalMin] = values.map(Number);
    try {
      const url = new URL(draft.delayTestUrl);
      if (!['http:', 'https:'].includes(url.protocol)) throw new Error();
      new RegExp(draft.infoNodePattern);
    } catch {
      showError('请输入有效的测速 URL 和匹配表达式');
      return;
    }
    if (values.some(value => !/^\d+$/.test(value)) || delayTestTimeout < 100 || delayTestTimeout > 60000 ||
      retentionDays < 1 || retentionDays > 3650 || snapshotIntervalMin > 10080) {
      showError('测速超时须为 100–60000，保留天数须为 1–3650，快照间隔须为 0–10080');
      return;
    }

    const valuesToSave = { ...draft, delayTestTimeout, retentionDays, snapshotIntervalMin };
    const changed = Object.fromEntries(Object.entries(valuesToSave).filter(([key, value]) => value !== prefs?.[key as keyof PrefsDraft]));
    setSavingDraft(true);
    void savePrefs(changed).then(() => {
      setDraftEdits({});
      showSuccess('参数已保存');
    }).catch(() => showError('保存参数失败')).finally(() => setSavingDraft(false));
  };

  useEffect(() => {
    const controller = new AbortController();
    void api.configs(controller.signal).then(value => {
      if (!controller.signal.aborted) setOriginal(value);
    }).catch(() => {
      if (!controller.signal.aborted) showError('加载内核配置失败');
    });
    return () => controller.abort();
  }, [configRevision, showError]);
  useEffect(() => {
    const controller = new AbortController();
    void api.stats(controller.signal).then(value => {
      if (!controller.signal.aborted) setStats(value);
    }).catch(() => {
      if (!controller.signal.aborted) showError('加载数据库统计失败');
    });
    return () => controller.abort();
  }, [statsRevision, showError]);

  const action = (fn: () => Promise<unknown>, refresh?: () => void) => void fn().then(() => {
    showSuccess('操作完成');
    refresh?.();
  }).catch(() => showError('操作失败'));
  const save = (p: Partial<Preferences>) => void savePrefs(p).catch(() => showError('保存偏好失败'));
  const config = original && { ...original, ...configEdits };
  const oldTun = (original?.tun ?? {}) as Record<string, unknown>;
  const tun = { ...oldTun, ...tunEdits };
  const change = (key: string, value: string | boolean) => setConfigEdits(edits => {
    const next = { ...edits };
    const originalValue = isNumericConfigField(key) ? String(original?.[key] ?? '') : original?.[key];
    if (value === originalValue) delete next[key];
    else next[key] = value;
    return next;
  });
  const changeTun = (key: string, value: string | boolean) => setTunEdits(edits => {
    const next = { ...edits };
    if (value === oldTun[key]) delete next[key];
    else next[key] = value;
    return next;
  });
  const patch = (changes: Partial<MihomoConfig>) => {
    setSavingConfig(true);
    void api.patchConfig(changes).then(() => {
      setOriginal(value => value && { ...value, ...changes });
      setConfigEdits({});
      setTunEdits({});
      refreshConfig();
      window.dispatchEvent(new Event('deck-mode'));
      showSuccess('内核配置已应用');
    }).catch(() => showError('应用内核配置失败')).finally(() => setSavingConfig(false));
  };
  const critical = (changes: Partial<MihomoConfig>) =>
    'allow-lan' in changes || 'bind-address' in changes ||
    String(changes.mode ?? '').toLowerCase() === 'direct' ||
    portFields.some(key => key in changes) || tun.enable !== oldTun.enable;
  const applyConfig = () => {
    if (!original || savingConfig) return;
    const changes: Partial<MihomoConfig> = {};
    for (const [key, raw] of Object.entries(configEdits)) {
      let value = raw;
      if (isNumericConfigField(key)) {
        const input = String(raw).trim();
        const number = Number(input);
        const isPort = portFields.includes(key);
        if (!/^\d+$/.test(input) || !Number.isSafeInteger(number) || (isPort && number > 65535)) {
          const label = fields.find(([name]) => name === key)?.[1];
          showError(isPort ? `${label}须为 0–65535 的整数` : 'Geo 更新间隔须为非负整数');
          return;
        }
        value = number;
      }
      if (value !== original[key]) changes[key] = value;
    }
    if (Object.keys(tunEdits).some(key => tun[key] !== oldTun[key])) changes.tun = tun;
    if (!Object.keys(changes).length) return;
    if (critical(changes)) setConfirm({ kind: 'config', changes });
    else patch(changes);
  };
  return <div className="space-y-5">
      <h1 className="text-xl font-semibold">设置</h1>
      <Card className="space-y-4 p-4">
        <h2 className="font-semibold">Mihomo 内核参数</h2>
        {config && <fieldset disabled={savingConfig} className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {fields.map(([key, label, options]) => <label key={key} className="space-y-1 text-sm">
              <span>
                {label}
              </span>
              {options ? <Select className="w-full" value={String(config[key] ?? '').toLowerCase()} onChange={e => change(key, e.target.value)}>
                {options.map(v => <option key={v} value={v}>
                  {optionLabels[v] ?? v}
                </option>)}
              </Select> : <Input className="w-full" inputMode={isNumericConfigField(key) ? 'numeric' : undefined} value={String(config[key] ?? '')} onChange={e => change(key, e.target.value)} />}
            </label>)}
          </div>
          <div className="flex flex-wrap gap-4">
            {switches.map(([key, label]) => <Switch key={key} label={label} checked={Boolean(config[key])} onChange={v => change(key, v)} />)}
          </div>
          <h3 className="font-medium">TUN</h3>
          <div className="flex flex-wrap gap-4">
            {([['enable', '启用 TUN'], ['auto-route', '自动路由'], ['auto-detect-interface', '自动检测接口']] as const).map(([key, label]) => <Switch key={key} label={label} checked={Boolean(tun[key])} onChange={v => changeTun(key, v)} />)}
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            {([['stack', '协议栈', ['system', 'gvisor', 'mixed']], ['dns-mode', 'DNS 模式', ['normal', 'fake-ip', 'redir-host']]] as [string, string, string[]][]).map(([key, label, options]) => <label key={key} className="space-y-1 text-sm">
              <span>
                {label}
              </span>
              <Select className="w-full" value={String(tun[key] ?? '')} onChange={e => changeTun(key, e.target.value)}>
                {options.map(v => <option key={v} value={v}>
                  {optionLabels[v] ?? v}
                </option>)}
              </Select>
            </label>)}
          </div>
          <Button onClick={applyConfig}>{savingConfig ? '应用中…' : '应用内核配置'}</Button>
        </fieldset>}
      </Card>
      <Card className="space-y-3 p-4">
        <h2 className="font-semibold">诊断与维护工具</h2>
        <div className="flex flex-wrap gap-2">
          <Input className="min-w-40 flex-1" placeholder="查询域名" value={domain} onChange={e => setDomain(e.target.value)} />
          <Select value={dnsType} onChange={e => setDnsType(e.target.value)}>
            {['A', 'AAAA', 'CNAME', 'TXT', 'MX', 'HTTPS'].map(v => <option key={v}>
              {v}
            </option>)}
          </Select>
          <Button disabled={!domain.trim()} onClick={() => void api.dns(domain, dnsType).then(v => setDnsResult(JSON.stringify(v, null, 2))).catch(() => showError('DNS 查询失败'))}>查询</Button>
        </div>
        {dnsResult && <pre className="overflow-auto whitespace-pre-wrap break-all text-xs">
          {dnsResult}
        </pre>}
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => action(api.flushFakeIP)}>清理 FakeIP 缓存</Button>
          <Button onClick={() => action(api.flushDNS)}>清理 DNS 缓存</Button>
          <Button onClick={() => action(api.updateGeo)}>更新 Geo 数据库</Button>
          <Button onClick={() => setConfirm({ kind: 'reload' })}>重载配置</Button>
          <Button onClick={() => setConfirm({ kind: 'restart' })}>重启内核</Button>
        </div>
      </Card>
      <Card className="space-y-4 p-4">
        <h2 className="font-semibold">持久化与控制台偏好</h2>
        {prefs && <>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {([['theme', '主题', ['system', 'light', 'dark']], ['proxySort', '节点排序', ['default', 'delay', 'name']], ['defaultConnectionsTab', '默认连接页', ['active', 'history']], ['logPersistLevel', '日志持久化级别', ['debug', 'info', 'warning', 'error', 'silent']]] as [keyof Preferences, string, string[]][]).map(([key, label, options]) => <label key={key} className="space-y-1 text-sm">
              <span>
                {label}
              </span>
              <Select className="w-full" value={String(prefs[key])} onChange={e => save({
              [key]: e.target.value
            })}>
                {options.map(v => <option key={v} value={v}>
                  {optionLabels[v] ?? v}
                </option>)}
              </Select>
            </label>)}
            {([['delayTestUrl', '测速 URL', false], ['delayTestTimeout', '测速超时（毫秒）', true], ['retentionDays', '保留天数', true], ['snapshotIntervalMin', '快照间隔（分钟）', true]] as [keyof typeof draft, string, boolean][]).map(([key, label, numeric]) => <label key={key} className="space-y-1 text-sm">
              <span>
                {label}
              </span>
              <Input className="w-full" type={numeric ? 'number' : 'text'} disabled={savingDraft} value={draft[key]} onChange={e => editDraft(key, e.target.value)} />
            </label>)}
          </div>
          <div className="flex flex-wrap gap-4">
            <Switch label="隐藏信息伪节点" checked={prefs.hideInfoNodes} onChange={v => save({
            hideInfoNodes: v
          })} />
            <Switch label="自动刷新概览" checked={prefs.autoRefreshOverview} onChange={v => save({
            autoRefreshOverview: v
          })} />
          </div>
          <div className="flex flex-wrap gap-2">
            <Input className="min-w-48 flex-1" aria-label="信息节点匹配表达式" disabled={savingDraft} value={draft.infoNodePattern} onChange={e => editDraft('infoNodePattern', e.target.value)} />
            <Button disabled={savingDraft} onClick={() => editDraft('infoNodePattern', '剩余流量|距离下次|套餐到期|官网|重置|到期|Traffic|Expire|Website')}>恢复默认表达式</Button>
            <Button disabled={!dirty || savingDraft} onClick={saveDraft}>{savingDraft ? '保存中…' : '保存参数'}</Button>
          </div>
        </>}
        {stats && <div className="text-sm text-zinc-500">
  数据库 
  {formatBytes(stats.fileSizeBytes)}
   · 采样 
  {stats.trafficSamples}
   · 小时汇总 
  {stats.trafficHourly}
   · 连接 
  {stats.connectionHistory}
   · 日志 
  {stats.logEntries}
   · 测速 
  {stats.delayHistory}
   · 订阅快照 
  {stats.providerSnapshots}
</div>}
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => action(api.prune, refreshStats)}>立即清理过期数据</Button>
          <Button onClick={() => setConfirm({ kind: 'connections' })}>清空历史连接</Button>
          <Button onClick={() => setConfirm({ kind: 'logs' })}>清空历史日志</Button>
        </div>
      </Card>
      <Card className="space-y-3 p-4">
        <h2 className="font-semibold">账户与安全</h2>
        <div className="text-sm text-zinc-500">
  用户：
  {username}
   · 上游：
  {status?.connected ? '已连接' : '未连接'}
   · 版本：
  {status?.version || '未知'}
</div>
        <form className="grid gap-2 sm:grid-cols-3" onSubmit={e => {
        e.preventDefault();
        const passwordBytes = new TextEncoder().encode(password).length;
        if (passwordBytes < 8 || passwordBytes > 72 || password !== repeat) {
          showError('新密码须为 8–72 个 UTF-8 字节，且两次输入须一致');
          return;
        }
        void updatePassword(current, password).then(() => {
          setCurrent('');
          setPassword('');
          setRepeat('');
          showSuccess('密码已更新');
        }).catch(() => showError('修改密码失败'));
      }}>
          <Input type="password" autoComplete="current-password" placeholder="当前密码" value={current} onChange={e => setCurrent(e.target.value)} required />
          <Input type="password" autoComplete="new-password" placeholder="新密码（8–72 字节）" value={password} onChange={e => setPassword(e.target.value)} required />
          <Input type="password" autoComplete="new-password" placeholder="确认新密码" value={repeat} onChange={e => setRepeat(e.target.value)} required />
          <Button type="submit">修改密码</Button>
        </form>
        <Button onClick={() => void logout()}>退出登录</Button>
      </Card>
      <ConfirmDialog open={!!confirm} onClose={() => setConfirm(null)} title="确认操作" danger onConfirm={() => {
      if (confirm?.kind === 'config') patch(confirm.changes);
      if (confirm?.kind === 'reload') action(() => api.reloadConfig(), refreshConfig);
      if (confirm?.kind === 'restart') action(async () => {
        await api.restart();
        await new Promise(resolve => setTimeout(resolve, 1500));
      }, refreshConfig);
      if (confirm?.kind === 'connections') action(api.clearConnections, refreshStats);
      if (confirm?.kind === 'logs') action(api.clearLogs, refreshStats);
    }}>此操作可能影响连接或删除历史数据，确定继续吗？</ConfirmDialog>
    </div>;
}
