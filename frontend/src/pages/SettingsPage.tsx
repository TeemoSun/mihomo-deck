import { useEffect, useState } from 'react';
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
export default function SettingsPage() {
  const [config, setConfig] = useState<MihomoConfig | null>(null);
  const [original, setOriginal] = useState<MihomoConfig | null>(null);
  const [stats, setStats] = useState<DBStats | null>(null);
  const [domain, setDomain] = useState('');
  const [dnsType, setDnsType] = useState('A');
  const [dnsResult, setDnsResult] = useState('');
  const [confirm, setConfirm] = useState('');
  const [current, setCurrent] = useState('');
  const [password, setPassword] = useState('');
  const [repeat, setRepeat] = useState('');
  const {
    prefs,
    savePrefs
  } = usePrefsStore();
  const {
    user,
    updatePassword,
    logout
  } = useAuthStore();
  const status = useLiveStore(s => s.status);
  const toast = useToastStore();
  const load = () => {
    void api.configs().then(v => {
      setConfig(v);
      setOriginal(v);
    }).catch(() => toast.error('加载内核配置失败'));
    void api.stats().then(setStats).catch(() => toast.error('加载数据库统计失败'));
  };
  useEffect(load, []);
  const action = (fn: () => Promise<unknown>) => void fn().then(() => {
    toast.success('操作完成');
    load();
  }).catch(() => toast.error('操作失败'));
  const save = (p: Partial<Preferences>) => void savePrefs(p).catch(() => toast.error('保存偏好失败'));
  const change = (key: string, value: unknown) => setConfig(c => c ? {
    ...c,
    [key]: value
  } : c);
  const tun = (config?.tun || {}) as Record<string, unknown>;
  const oldTun = (original?.tun || {}) as Record<string, unknown>;
  const changeTun = (key: string, value: unknown) => change('tun', {
    ...tun,
    [key]: value
  });
  const patch = () => {
    if (!config || !original) return;
    const changed = Object.fromEntries(Object.entries(config).filter(([key, value]) => JSON.stringify(value) !== JSON.stringify(original[key])));
    if (Object.keys(changed).length) action(() => api.patchConfig(changed));
  };
  const critical = () => !!config && !!original && (original['allow-lan'] === true && config['allow-lan'] === false || config.mode === 'direct' && original.mode !== 'direct' || ['mixed-port', 'port', 'socks-port', 'redir-port', 'tproxy-port'].some(k => config[k] !== original[k]) || tun.enable !== oldTun.enable);
  return <div className="space-y-5">
      <h1 className="text-xl font-semibold">设置</h1>
      <Card className="space-y-4 p-4">
        <h2 className="font-semibold">Mihomo 内核参数</h2>
        {config && <>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {fields.map(([key, label, options]) => <label key={key} className="space-y-1 text-sm">
              <span>
                {label}
              </span>
              {options ? <Select className="w-full" value={String(config[key] ?? '')} onChange={e => change(key, e.target.value)}>
                {options.map(v => <option key={v}>
                  {v}
                </option>)}
              </Select> : <Input className="w-full" type={key.includes('port') || key === 'geo-update-interval' ? 'number' : 'text'} value={String(config[key] ?? '')} onChange={e => change(key, key.includes('port') || key === 'geo-update-interval' ? Number(e.target.value) : e.target.value)} />}
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
                {options.map(v => <option key={v}>
                  {v}
                </option>)}
              </Select>
            </label>)}
          </div>
          <Button onClick={() => critical() ? setConfirm('config') : patch()}>应用内核配置</Button>
        </>}
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
          <Button disabled={!domain.trim()} onClick={() => void api.dns(domain, dnsType).then(v => setDnsResult(JSON.stringify(v, null, 2))).catch(() => toast.error('DNS 查询失败'))}>查询</Button>
        </div>
        {dnsResult && <pre className="overflow-auto whitespace-pre-wrap break-all text-xs">
          {dnsResult}
        </pre>}
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => action(api.flushFakeIP)}>清理 FakeIP 缓存</Button>
          <Button onClick={() => action(api.flushDNS)}>清理 DNS 缓存</Button>
          <Button onClick={() => action(api.updateGeo)}>更新 Geo 数据库</Button>
          <Button onClick={() => setConfirm('reload')}>重载配置</Button>
          <Button onClick={() => setConfirm('restart')}>重启内核</Button>
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
                {options.map(v => <option key={v}>
                  {v}
                </option>)}
              </Select>
            </label>)}
            {([['delayTestUrl', '测速 URL', false], ['delayTestTimeout', '测速超时（毫秒）', true], ['retentionDays', '保留天数', true], ['snapshotIntervalMin', '快照间隔（分钟）', true]] as [keyof Preferences, string, boolean][]).map(([key, label, numeric]) => <label key={key} className="space-y-1 text-sm">
              <span>
                {label}
              </span>
              <Input className="w-full" type={numeric ? 'number' : 'text'} defaultValue={String(prefs[key])} key={`${key}-${prefs[key]}`} onBlur={e => save({
              [key]: numeric ? Number(e.target.value) : e.target.value
            })} />
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
            <Input className="min-w-48 flex-1" aria-label="信息节点匹配表达式" defaultValue={prefs.infoNodePattern} key={prefs.infoNodePattern} onBlur={e => save({
            infoNodePattern: e.target.value
          })} />
            <Button onClick={() => save({
            infoNodePattern: '剩余流量|距离下次|套餐到期|官网|重置|到期|Traffic|Expire|Website'
          })}>恢复默认表达式</Button>
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
          <Button onClick={() => action(api.prune)}>立即清理过期数据</Button>
          <Button onClick={() => setConfirm('connections')}>清空历史连接</Button>
          <Button onClick={() => setConfirm('logs')}>清空历史日志</Button>
        </div>
      </Card>
      <Card className="space-y-3 p-4">
        <h2 className="font-semibold">账户与安全</h2>
        <div className="text-sm text-zinc-500">
  用户：
  {user?.username}
   · 上游：
  {status?.connected ? '已连接' : '未连接'}
   · 版本：
  {status?.version || '未知'}
</div>
        <form className="grid gap-2 sm:grid-cols-3" onSubmit={e => {
        e.preventDefault();
        if (password.length < 8 || password !== repeat) {
          toast.error('新密码至少 8 个字符且两次输入须一致');
          return;
        }
        void updatePassword(current, password).then(() => {
          setCurrent('');
          setPassword('');
          setRepeat('');
          toast.success('密码已更新');
        }).catch(() => toast.error('修改密码失败'));
      }}>
          <Input type="password" autoComplete="current-password" placeholder="当前密码" value={current} onChange={e => setCurrent(e.target.value)} required />
          <Input type="password" autoComplete="new-password" placeholder="新密码（至少 8 位）" value={password} onChange={e => setPassword(e.target.value)} required />
          <Input type="password" autoComplete="new-password" placeholder="确认新密码" value={repeat} onChange={e => setRepeat(e.target.value)} required />
          <Button type="submit">修改密码</Button>
        </form>
        <Button onClick={() => void logout()}>退出登录</Button>
      </Card>
      <ConfirmDialog open={!!confirm} onClose={() => setConfirm('')} title="确认操作" danger onConfirm={() => {
      if (confirm === 'config') patch();
      if (confirm === 'reload') action(() => api.reloadConfig());
      if (confirm === 'restart') action(api.restart);
      if (confirm === 'connections') action(api.clearConnections);
      if (confirm === 'logs') action(api.clearLogs);
    }}>此操作可能影响连接或删除历史数据，确定继续吗？</ConfirmDialog>
    </div>;
}
