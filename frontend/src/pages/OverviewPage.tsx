import { useEffect, useState } from 'react';
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { api } from '../api/client';
import type { OverviewResponse, TrafficPoint, ProxyItem, ProviderSnapshot } from '../types/api';
import { usePrefsStore } from '../stores/usePrefsStore';
import { useLiveStore } from '../stores/useLiveStore';
import { useToastStore } from '../stores/useToastStore';
import { formatBytes, formatSpeed, formatRelative, formatUnixTime } from '../lib/format';
import { Badge, Button, Card, EmptyState, Modal, Select, Tabs } from '../components/ui';
import ModeSwitcher from '../components/ModeSwitcher';
export default function OverviewPage() {
  const [data, setData] = useState<OverviewResponse | null>(null);
  const [range, setRange] = useState('live');
  const [chart, setChart] = useState('network');
  const [series, setSeries] = useState<TrafficPoint[]>([]);
  const [proxies, setProxies] = useState<Record<string, ProxyItem>>({});
  const [history, setHistory] = useState<ProviderSnapshot[] | null>(null);
  const [providerName, setProviderName] = useState('');
  const prefs = usePrefsStore(s => s.prefs);
  const autoRefresh = usePrefsStore(s => s.prefs?.autoRefreshOverview);
  const live = useLiveStore();
  const toast = useToastStore();
  const load = () => {
    if (range !== 'live') void api.traffic(range).then(setSeries).catch(() => toast.error('加载流量历史失败'));
    void api.overview().then(setData).catch(() => toast.error('加载概览失败'));
    void api.proxies().then(x => setProxies(x.proxies)).catch(() => {});
  };
  useEffect(() => {
    load();
    if (autoRefresh === false) return;
    const id = setInterval(load, 60000);
    return () => clearInterval(id);
  }, [autoRefresh, range]);
  const points = range === 'live' ? live.samples : series.map(p => ({
    ...p,
    upBps: p.upBps || p.peakUpBps,
    downBps: p.downBps || p.peakDownBps,
    memInuse: p.memInuse || p.avgMem
  }));
  const sum = data?.trafficSummary;
  const groups = Object.values(proxies).filter(p => ['Selector', 'URLTest'].includes(p.type) && p.all?.length && !prefs?.hiddenGroups.includes(p.name) && (p.name !== 'GLOBAL' || data?.configs?.mode?.toLowerCase() === 'global')).sort((a, b) => Number(b.type === 'Selector') - Number(a.type === 'Selector'));
  const stats = data?.topStats;
  return <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold">概览</h1>
        <p className="mt-1 text-sm text-zinc-500">运行状态与流量概况</p>
      </div>
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {[
          { label: '实时速率', value: formatSpeed(live.traffic.up + live.traffic.down), detail: `↑ ${formatSpeed(live.traffic.up)} · ↓ ${formatSpeed(live.traffic.down)}` },
          { label: '内存占用', value: formatBytes(live.memory.inuse), detail: `上限 ${live.memory.oslimit ? formatBytes(live.memory.oslimit) : '不限'}` },
          { label: '活跃连接数', value: String(live.connections.length) },
          { label: '上游核心', value: live.status?.connected ? '已连接' : '离线', detail: live.status?.version || '版本未知' },
          { label: '今日流量', value: formatBytes((sum?.todayUp || 0) + (sum?.todayDown || 0)), detail: `↑ ${formatBytes(sum?.todayUp || 0)} · ↓ ${formatBytes(sum?.todayDown || 0)}` },
          { label: '近 7 天流量', value: formatBytes((sum?.weekUp || 0) + (sum?.weekDown || 0)) },
          { label: '近 30 天流量', value: formatBytes((sum?.monthUp || 0) + (sum?.monthDown || 0)) },
          { label: '跨重启累计流量', value: formatBytes((sum?.cumulativeUp || 0) + (sum?.cumulativeDown || 0)), detail: `↑ ${formatBytes(sum?.cumulativeUp || 0)} · ↓ ${formatBytes(sum?.cumulativeDown || 0)}` }
        ].map(({label, value, detail}) => <Card key={label} className="p-4"><div className="text-xs text-zinc-500">{label}</div><div className="mt-2 text-lg font-semibold">{value}</div>{detail && <div className="mt-1 text-xs text-zinc-500">{detail}</div>}</Card>)}
      </div>
      <Card className="p-4">
        <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
          <h2 className="font-semibold">流量与内存</h2>
          <Tabs value={chart} onChange={setChart} items={[{
          value: 'network',
          label: '网络速率 / 流量'
        }, {
          value: 'memory',
          label: '内存占用'
        }]} />
          <Tabs value={range} onChange={setRange} items={[{
          value: 'live',
          label: '实时 (60s)'
        }, {
          value: '1h',
          label: '1小时'
        }, {
          value: '24h',
          label: '24小时'
        }, {
          value: '7d',
          label: '7天'
        }, {
          value: '30d',
          label: '30天'
        }]} />
        </div>
        <div className="mb-2 text-xs text-zinc-500">
          {chart === 'memory' ? '内存占用' : ['7d', '30d'].includes(range) ? '下载流量 · 上传流量（每小时）' : '下载速率 · 上传速率'}
        </div>
        <div className="h-64">
          {points.length ? <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={points}>
              <CartesianGrid strokeDasharray="3 3" strokeOpacity={0.12} />
              <XAxis dataKey="ts" tickFormatter={v => (['7d', '30d'].includes(range) ? new Date(v * 1000).toLocaleDateString('zh-CN', {month:'2-digit', day:'2-digit'}) + ' ' : '') + new Date(v * 1000).toLocaleTimeString('zh-CN', {hour:'2-digit', minute:'2-digit', hour12:false})} tick={{
              fontSize: 10
            }} minTickGap={35} />
              <YAxis tickFormatter={chart === 'network' && !['7d', '30d'].includes(range) ? formatSpeed : formatBytes} tick={{
              fontSize: 10
            }} width={70} />
              <Tooltip labelFormatter={v => formatUnixTime(Number(v))} formatter={(v, name) => [chart === 'network' && !['7d', '30d'].includes(range) ? formatSpeed(Number(v)) : formatBytes(Number(v)), name]} />
              {chart === 'network' ? <>
                <Area type="monotone" dataKey={['7d', '30d'].includes(range) ? 'downBytes' : 'downBps'} name={['7d', '30d'].includes(range) ? '下载流量' : '下载速率'} stroke="#6366f1" fill="#6366f1" fillOpacity={0.12} />
                <Area type="monotone" dataKey={['7d', '30d'].includes(range) ? 'upBytes' : 'upBps'} name={['7d', '30d'].includes(range) ? '上传流量' : '上传速率'} stroke="#10b981" fill="#10b981" fillOpacity={0.08} />
              </> : <Area type="monotone" dataKey="memInuse" name="内存占用" stroke="#f59e0b" fill="#f59e0b" fillOpacity={0.1} />}
            </AreaChart>
          </ResponsiveContainer> : <EmptyState>暂无曲线数据</EmptyState>}
        </div>
      </Card>
      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="p-4">
          <h2 className="mb-4 font-semibold">快速模式</h2>
          <ModeSwitcher mode={data?.configs?.mode || 'rule'} onChange={mode => setData(d => d ? {
          ...d,
          configs: {
            ...d.configs,
            mode
          }
        } : d)} />
        </Card>
        <Card className="p-4">
          <h2 className="mb-4 font-semibold">快速切换策略组</h2>
          {groups.length ? groups.slice(0, 4).map(g => <div key={g.name} className="mb-2 flex items-center gap-2">
            <span className="w-28 truncate text-sm" title={g.name}>
              {g.name}
            </span>
            <Select aria-label={`${g.name} 节点`} value={g.now || ''} onChange={e => void api.selectProxy(g.name, e.target.value).then(load).catch(() => toast.error('切换节点失败'))} className="min-w-0 flex-1">
              {g.all?.map(n => <option key={n}>
                {n}
              </option>)}
            </Select>
            {g.fixed && <><Badge>已固定</Badge><Button onClick={() => void api.unfixProxy(g.name).then(load).catch(() => toast.error('取消固定失败'))}>取消固定</Button></>}
            <Button onClick={() => void api.testGroup(g.name).then(() => {
            toast.success('测速完成');
            load();
          }).catch(() => toast.error('测速失败'))}>测速</Button>
          </div>) : <EmptyState>暂无可用策略组</EmptyState>}
        </Card>
      </div>
      <div>
        <h2 className="mb-3 font-semibold">订阅用量</h2>
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {Object.values(data?.providers?.providers || {}).filter(p => p.subscriptionInfo).map(p => {
          const s = p.subscriptionInfo!;
          const used = s.Upload + s.Download;
          return <Card key={p.name} className="p-4">
            <div className="flex items-center justify-between gap-2">
              <button className="truncate text-left font-medium hover:text-indigo-500" onClick={() => {
                setProviderName(p.name);
                void api.providerHistory(p.name).then(setHistory).catch(() => toast.error('加载订阅趋势失败'));
              }}>
                {p.name}
              </button>
              <Button onClick={() => void api.updateProvider(p.name).then(() => {
                toast.success('订阅已更新');
                load();
              }).catch(() => toast.error('更新订阅失败'))}>更新</Button>
            </div>
            <div className="mt-4 h-1.5 overflow-hidden rounded-full bg-zinc-100 dark:bg-zinc-800">
              <div className="h-full bg-indigo-500" style={{
                width: `${s.Total ? Math.min(100, used / s.Total * 100) : 0}%`
              }} />
            </div>
            <div className="mt-2 flex justify-between text-xs text-zinc-500">
              <span>已用 {formatBytes(used)} / {s.Total ? formatBytes(s.Total) : '未知'}</span>
              <span>剩余 {s.Total ? formatBytes(Math.max(0, s.Total - used)) : '未知'}</span>
            </div>
            <div className="mt-3 text-xs text-zinc-500">到期：<span title={s.Expire ? formatUnixTime(s.Expire) : undefined}>{s.Expire ? formatRelative(s.Expire) : '未知'}</span> · 可用节点 {(p.proxies || []).filter(n => n.alive).length} / {(p.proxies || []).length}</div>
          </Card>;
        })}
        </div>
        {!Object.values(data?.providers?.providers || {}).some(p => p.subscriptionInfo) && <EmptyState>暂无订阅用量信息</EmptyState>}
      </div>
      <div>
        <h2 className="mb-3 font-semibold">近 24 小时排行</h2>
        <div className="grid gap-3 md:grid-cols-3">
          {([['Top 域名', stats?.topHosts], ['Top 节点', stats?.topProxies], ['Top 客户端 IP', stats?.topSrcIps]] as const).map(([title, items]) => <Card key={title} className="p-4">
            <h3 className="mb-3 text-sm font-medium">
              {title}
            </h3>
            {items?.length ? items.slice(0, 5).map(x => <div key={x.name} className="flex justify-between gap-2 border-t border-zinc-100 py-2 text-xs dark:border-zinc-800">
              <span className="truncate">
                {x.name}
              </span>
              <span className="whitespace-nowrap text-zinc-500">
                {formatBytes(x.bytes)} · {x.count} 次
              </span>
            </div>) : <EmptyState>暂无数据</EmptyState>}
          </Card>)}
        </div>
      </div>
      <Modal open={history !== null} onClose={() => setHistory(null)} title={`${providerName} · 订阅趋势`}>
        <div className="h-56">
          {history?.length ? <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={[...history].reverse()}>
              <XAxis dataKey="capturedAt" tickFormatter={v => new Date(v * 1000).toLocaleDateString('zh-CN')} tick={{
              fontSize: 10
            }} />
              <YAxis tickFormatter={formatBytes} tick={{
              fontSize: 10
            }} width={65} />
              <Tooltip labelFormatter={v => formatUnixTime(Number(v))} formatter={v => formatBytes(Number(v))} />
              <Area dataKey="downloadBytes" name="下载" stroke="#6366f1" fill="#6366f1" fillOpacity={0.15} />
              <Area dataKey="uploadBytes" name="上传" stroke="#10b981" fill="none" />
            </AreaChart>
          </ResponsiveContainer> : <EmptyState>暂无快照</EmptyState>}
        </div>
      </Modal>
    </div>;
}
