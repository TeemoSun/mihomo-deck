import { useEffect, useState } from 'react';
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { api } from '../api/client';
import { Badge, Button, Card, EmptyState, Input, Modal, Tabs } from '../components/ui';
import { formatBytes, formatRelative, formatUnixTime } from '../lib/format';
import { useToastStore } from '../stores/useToastStore';
import type { ProviderSnapshot, ProxyProvider, RuleItem, RuleProvider } from '../types/api';
export default function ProvidersPage() {
  const [tab, setTab] = useState('proxy');
  const [providers, setProviders] = useState<ProxyProvider[]>([]);
  const [ruleProviders, setRuleProviders] = useState<RuleProvider[]>([]);
  const [rules, setRules] = useState<RuleItem[]>([]);
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(1);
  const [busy, setBusy] = useState<string[]>([]);
  const [history, setHistory] = useState<ProviderSnapshot[] | null>(null);
  const [name, setName] = useState('');
  const toast = useToastStore();
  const load = () => {
    void api.proxyProviders().then(v => setProviders(Object.values(v.providers).filter(p => p.vehicleType !== 'Compatible'))).catch(() => toast.error('加载代理订阅失败'));
    void api.ruleProviders().then(v => setRuleProviders(Object.values(v.providers))).catch(() => toast.error('加载规则集失败'));
    void api.rules().then(v => setRules(v.rules || [])).catch(() => toast.error('加载路由规则失败'));
  };
  useEffect(load, []);
  const action = (key: string, fn: () => Promise<unknown>) => { setBusy(b => [...b, key]); void fn().then(() => {
    toast.success('操作完成');
    load();
  }).catch(() => toast.error('操作失败')).finally(() => setBusy(b => b.filter(x => x !== key))); };
  const filteredRules = rules.map((r, i) => ({r, i})).filter(({r}) => `${r.type} ${r.payload} ${r.proxy}`.toLowerCase().includes(query.toLowerCase()));
  return <div className="space-y-4">
      <h1 className="text-xl font-semibold">订阅与规则</h1>
      <Tabs value={tab} onChange={setTab} items={[{
      value: 'proxy',
      label: '代理订阅'
    }, {
      value: 'providers',
      label: '规则集'
    }, {
      value: 'rules',
      label: '路由规则'
    }]} />
      {tab === 'proxy' && <div className="grid gap-3 lg:grid-cols-2">
        {providers.map(p => {
        const info = p.subscriptionInfo;
        const used = info ? info.Upload + info.Download : 0;
        return <Card key={p.name} className="space-y-3 p-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h2 className="font-semibold">
              {p.name}
            </h2>
            <Badge>
              {p.vehicleType || p.type}
            </Badge>
          </div>
          <div className="text-xs text-zinc-500">
  更新：
  {p.updatedAt ? new Date(p.updatedAt).toLocaleString('zh-CN') : '未知'}
   · 可用 
  {(p.proxies || []).filter(n => n.alive).length}
   / 
  {(p.proxies || []).length}
</div>
          {info && <>
            <div className="h-1.5 overflow-hidden rounded bg-zinc-100 dark:bg-zinc-800">
              <div className="h-full bg-indigo-500" style={{
                width: `${info.Total ? Math.min(100, used / info.Total * 100) : 0}%`
              }} />
            </div>
            <div className="text-xs text-zinc-500">
  已用 
  {formatBytes(used)}
   / 
  {info.Total ? formatBytes(info.Total) : '未知'}
   · 到期 
  {info.Expire ? formatRelative(info.Expire) : '未知'}
</div>
          </>}
          <div className="flex flex-wrap gap-2">
            <Button disabled={busy.includes(`update:${p.name}`)} onClick={() => action(`update:${p.name}`, () => api.updateProvider(p.name))}>{busy.includes(`update:${p.name}`) ? '更新中…' : '更新订阅'}</Button>
            <Button disabled={busy.includes(`health:${p.name}`)} onClick={() => action(`health:${p.name}`, () => api.healthProvider(p.name))}>{busy.includes(`health:${p.name}`) ? '检查中…' : '健康检查'}</Button>
            <Button onClick={() => {
              setName(p.name);
              void api.providerHistory(p.name).then(setHistory).catch(() => toast.error('加载历史趋势失败'));
            }}>历史趋势</Button>
          </div>
          <details>
            <summary className="cursor-pointer text-sm text-zinc-500">节点列表</summary>
            <div className="mt-2 flex flex-wrap gap-1">
              {(p.proxies || []).map(n => <Badge key={n.name}>
  {n.name}
   · 
  {n.alive ? '可用' : '未知或不可用'}
</Badge>)}
            </div>
          </details>
        </Card>;
      })}
        {!providers.length && <EmptyState>暂无代理订阅</EmptyState>}
      </div>}
      {tab === 'providers' && <div className="grid gap-3 md:grid-cols-2">
        {ruleProviders.map(p => <Card key={p.name} className="space-y-3 p-4">
          <div className="flex justify-between gap-2">
            <h2 className="font-semibold">
              {p.name}
            </h2>
            <Badge>
              {p.behavior}
            </Badge>
          </div>
          <div className="text-sm text-zinc-500">
  {p.vehicleType}
   · 
  {p.ruleCount}
   条规则 · 更新：
  {p.updatedAt ? new Date(p.updatedAt).toLocaleString('zh-CN') : '未知'}
</div>
          <Button disabled={busy.includes(`rule:${p.name}`)} onClick={() => action(`rule:${p.name}`, () => api.updateRuleProvider(p.name))}>{busy.includes(`rule:${p.name}`) ? '更新中…' : '更新规则集'}</Button>
        </Card>)}
        {!ruleProviders.length && <EmptyState>暂无规则集</EmptyState>}
      </div>}
      {tab === 'rules' && <>
        <Input className="w-full" placeholder="搜索规则" value={query} onChange={e => { setQuery(e.target.value); setPage(1); }} />
        <Card className="overflow-hidden">
          {filteredRules.slice((page - 1) * 100, page * 100).map(({r, i}) => <div key={i} className="grid gap-1 border-b border-zinc-100 p-3 text-sm dark:border-zinc-800 md:grid-cols-[4rem_10rem_1fr_12rem_auto]">
            <span className="text-zinc-400">
  #
  {i + 1}
</span>
            <span>
              {r.type}
            </span>
            <span className="break-all">
              {r.payload}
            </span>
            <span>
              {r.proxy}
            </span>
            <span className="flex gap-1">
              {r.extra?.hitCount !== undefined && <Badge>
  命中 
  {r.extra.hitCount}
</Badge>}
              {r.extra?.missCount !== undefined && <Badge>
  未命中 
  {r.extra.missCount}
</Badge>}
            </span>
          </div>)}
        </Card>
        <div className="flex items-center justify-end gap-2 text-sm"><span>共 {filteredRules.length} 条 · 第 {page} 页</span><Button disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</Button><Button disabled={page * 100 >= filteredRules.length} onClick={() => setPage(page + 1)}>下一页</Button></div>
      </>}
      <Modal open={history !== null} onClose={() => setHistory(null)} title={`${name} · 历史趋势`}>
        {history?.length ? <><div className="h-52"><ResponsiveContainer width="100%" height="100%"><AreaChart data={[...history].reverse()}><CartesianGrid strokeDasharray="3 3" strokeOpacity={0.12}/><XAxis dataKey="capturedAt" tickFormatter={v => new Date(v * 1000).toLocaleDateString('zh-CN')} tick={{fontSize:10}}/><YAxis tickFormatter={formatBytes} tick={{fontSize:10}} width={65}/><Tooltip labelFormatter={v => formatUnixTime(Number(v))} formatter={v => formatBytes(Number(v))}/><Area dataKey="downloadBytes" name="下载" stroke="#6366f1" fill="#6366f1" fillOpacity={0.15}/><Area dataKey="uploadBytes" name="上传" stroke="#10b981" fill="none"/></AreaChart></ResponsiveContainer></div><div className="space-y-2">
          {history.map(h => <div key={h.id} className="flex flex-wrap justify-between gap-2 border-b py-2 text-sm">
            <span>
              {formatUnixTime(h.capturedAt)}
            </span>
            <span>
  上传 
  {formatBytes(h.uploadBytes)}
   · 下载 
  {formatBytes(h.downloadBytes)}
   · 可用 
  {h.aliveCount}
  /
  {h.nodeCount}
</span>
          </div>)}
        </div></> : <EmptyState>暂无快照</EmptyState>}
      </Modal>
    </div>;
}
