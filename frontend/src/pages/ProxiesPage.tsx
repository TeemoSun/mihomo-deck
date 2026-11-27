import { useEffect, useState } from 'react';
import { Area, AreaChart, CartesianGrid, XAxis, YAxis, ResponsiveContainer, Tooltip } from 'recharts';
import { ChevronDown, ChevronRight, Star } from 'lucide-react';
import { api } from '../api/client';
import type { DelayRecord, ProxyItem } from '../types/api';
import { usePrefsStore } from '../stores/usePrefsStore';
import { useToastStore } from '../stores/useToastStore';
import { delayTone, isInfoPseudoNode, formatUnixTime } from '../lib/format';
import { Badge, Button, Card, EmptyState, Input, Modal, Select, Switch } from '../components/ui';
import ModeSwitcher from '../components/ModeSwitcher';
export default function ProxiesPage() {
  const [proxies, setProxies] = useState<Record<string, ProxyItem>>({});
  const [mode, setMode] = useState('rule');
  const [search, setSearch] = useState('');
  const [onlyPinned, setOnlyPinned] = useState(false);
  const [collapsed, setCollapsed] = useState<string[]>([]);
  const [history, setHistory] = useState<DelayRecord[] | null>(null);
  const [historyName, setHistoryName] = useState('');
  const [busy, setBusy] = useState<string[]>([]);
  const {
    prefs,
    savePrefs
  } = usePrefsStore();
  const toast = useToastStore();
  const load = () => {
    void Promise.all([api.proxies(), api.proxyProviders().catch(() => null)]).then(([proxiesRes, providersRes]) => {
      const nodes: Record<string, ProxyItem> = {};
      for (const provider of Object.values(providersRes?.providers || {})) {
        for (const node of provider.proxies || []) {
          if (provider.vehicleType === 'Compatible' && nodes[node.name]) continue;
          nodes[node.name] = { ...node, 'provider-name': node['provider-name'] || provider.name };
        }
      }
      setProxies({ ...nodes, ...proxiesRes.proxies });
    }).catch(() => toast.error('加载节点失败'));
    void api.configs().then(x => setMode(x.mode)).catch(() => {});
  };
  useEffect(load, []);
  const save = (p: Parameters<typeof savePrefs>[0]) => void savePrefs(p).catch(() => toast.error('保存偏好失败'));
  const pinned = prefs?.pinnedProxies || [];
  const pin = (name: string) => save({
    pinnedProxies: pinned.includes(name) ? pinned.filter(x => x !== name) : [...pinned, name]
  });
  const action = (key: string, fn: () => Promise<unknown>) => {
    setBusy(b => [...b, key]);
    void fn().then(() => {
      toast.success('操作完成');
      load();
    }).catch(() => toast.error('操作失败')).finally(() => setBusy(b => b.filter(x => x !== key)));
  };
  const allGroups = Object.values(proxies).filter(p => ['Selector', 'URLTest', 'Fallback', 'LoadBalance'].includes(p.type) && p.all);
  const hiddenGroups = prefs?.hiddenGroups || [];
  const order = proxies.GLOBAL?.all || [];
  const groups = allGroups.filter(g => !hiddenGroups.includes(g.name)).sort((a, b) => {
    if (a.name === 'GLOBAL') return mode.toLowerCase() === 'global' ? -1 : 1;
    if (b.name === 'GLOBAL') return mode.toLowerCase() === 'global' ? 1 : -1;
    return Number(b.type === 'Selector') - Number(a.type === 'Selector') || (order.indexOf(a.name) < 0 ? Infinity : order.indexOf(a.name)) - (order.indexOf(b.name) < 0 ? Infinity : order.indexOf(b.name));
  });
  const hiddenCount = groups.reduce((n, g) => n + (g.all || []).filter(x => isInfoPseudoNode(x, prefs?.infoNodePattern || '')).length, 0);
  function node(name: string, group?: ProxyItem) {
    const p = proxies[name] || {
      name,
      type: '未知'
    };
    const delay = p.history?.at(-1)?.delay || 0;
    return <div key={`${group?.name || 'p'}-${name}`} className={`flex min-w-0 items-center gap-1 rounded-md border p-2 text-sm ${group?.now === name ? 'border-indigo-400 bg-indigo-50 dark:bg-indigo-950/40' : 'border-zinc-200 dark:border-zinc-800'}`}>
      <button className="min-w-0 flex-1 text-left" title={name} onClick={() => group && action(name, () => api.selectProxy(group.name, name))}>
        <span className="block truncate">
          {name}
        </span>
        <span className="text-xs text-zinc-400">
          {p.type}
          {p.udp ? ' · UDP' : ''}{p.now ? ` · 当前 ${p.now}` : ''}
        </span>
      </button>
      <button aria-label={pinned.includes(name) ? '取消收藏' : '收藏节点'} onClick={() => pin(name)} className={pinned.includes(name) ? 'text-amber-500' : 'text-zinc-300'}>
        <Star size={15} fill={pinned.includes(name) ? 'currentColor' : 'none'} />
      </button>
      <button title="查看延迟历史" className={`whitespace-nowrap text-xs ${delayTone(delay, p.alive)}`} onClick={() => {
        setHistoryName(name);
        void api.delays(name).then(setHistory).catch(() => toast.error('加载延迟历史失败'));
      }}>
        {delay ? `${delay} ms` : '—'}
      </button>
      <Button disabled={busy.includes(`test:${name}`)} onClick={() => action(`test:${name}`, () => api.testProxy(name, p['provider-name']))}>{busy.includes(`test:${name}`) ? '测速中…' : '测速'}</Button>
    </div>;
  }
  return <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">节点代理</h1>
          <p className="mt-1 text-sm text-zinc-500">管理策略组与节点</p>
        </div>
        <ModeSwitcher mode={mode} onChange={setMode} />
      </div>
      <Card className="flex flex-wrap items-center gap-3 p-3">
        <Input placeholder="搜索节点或策略组" aria-label="搜索节点或策略组" value={search} onChange={e => setSearch(e.target.value)} className="min-w-48 flex-1" />
        <Select aria-label="排序" value={prefs?.proxySort || 'default'} onChange={e => save({
        proxySort: e.target.value
      })}>
          <option value="default">默认排序</option>
          <option value="delay">延迟排序</option>
          <option value="name">名称排序</option>
        </Select>
        <Switch label="隐藏信息伪节点" checked={prefs?.hideInfoNodes || false} onChange={v => save({
        hideInfoNodes: v
      })} />
        <Badge>隐藏 {prefs?.hideInfoNodes ? hiddenCount : 0}</Badge>
        <Switch label="仅看收藏" checked={onlyPinned} onChange={setOnlyPinned} />
        <Button onClick={() => setCollapsed(collapsed.length ? [] : groups.map(g => g.name))}>
          {collapsed.length ? '展开全部' : '折叠全部'}
        </Button>
      </Card>
      {hiddenGroups.length > 0 && <div className="flex flex-wrap items-center gap-2 text-xs text-zinc-500">已隐藏策略组：{hiddenGroups.map(n => <Button key={n} onClick={() => save({
        hiddenGroups: hiddenGroups.filter(x => x !== n)
      })}>{n} · 显示</Button>)}</div>}
      {pinned.length > 0 && <Card className="p-4">
        <h2 className="mb-3 font-semibold">收藏节点</h2>
        <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
          {pinned.filter(n => n.toLowerCase().includes(search.toLowerCase())).map(n => {
          const matches = allGroups.filter(g => g.type === 'Selector' && g.all?.includes(n));
          return <div key={n}>
            {node(n, matches.length === 1 ? matches[0] : undefined)}
            {matches.length > 1 && <div className="mt-1 flex flex-wrap gap-1">
              {matches.map(g => <Button key={g.name} className={g.now === n ? '!border-indigo-400 !text-indigo-600' : ''} disabled={busy.includes(n)} onClick={() => action(n, () => api.selectProxy(g.name, n))}>
                {g.name}
                {g.now === n ? ' ✓' : ''}
              </Button>)}
            </div>}
          </div>;
        })}
        </div>
      </Card>}
      {groups.map(g => {
      let names = (g.all || []).filter(n => (!prefs?.hideInfoNodes || !isInfoPseudoNode(n, prefs.infoNodePattern)) && (!onlyPinned || pinned.includes(n)) && (g.name.toLowerCase().includes(search.toLowerCase()) || n.toLowerCase().includes(search.toLowerCase())));
      if (prefs?.proxySort === 'name') names = [...names].sort((a, b) => a.localeCompare(b));
      if (prefs?.proxySort === 'delay') names = [...names].sort((a, b) => (proxies[a]?.history?.at(-1)?.delay || Infinity) - (proxies[b]?.history?.at(-1)?.delay || Infinity));
      return <Card key={g.name} className="p-4">
        <div className="flex flex-wrap items-center gap-2">
          <button onClick={() => setCollapsed(collapsed.includes(g.name) ? collapsed.filter(x => x !== g.name) : [...collapsed, g.name])} className="flex min-w-0 flex-1 items-center gap-2 text-left font-semibold">
            {collapsed.includes(g.name) ? <ChevronRight size={16} /> : <ChevronDown size={16} />}
            <span className="truncate">
              {g.name}
            </span>
            <Badge>
              {g.type}
            </Badge>
          </button>
          <span className="text-xs text-zinc-500">当前：{g.now || '—'}</span>
          <Button disabled={busy.includes(`group:${g.name}`)} onClick={() => action(`group:${g.name}`, () => api.testGroup(g.name))}>{busy.includes(`group:${g.name}`) ? '测速中…' : '测速'}</Button>
          {g.fixed && <Button disabled={busy.includes(g.name)} onClick={() => action(g.name, () => api.unfixProxy(g.name))}>取消固定</Button>}
          <Button onClick={() => save({
            hiddenGroups: [...hiddenGroups, g.name]
          })}>隐藏</Button>
        </div>
        {!collapsed.includes(g.name) && (names.length ? <div className="mt-4 grid gap-2 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {names.map(n => node(n, g))}
        </div> : <EmptyState>没有匹配的节点</EmptyState>)}
      </Card>;
    })}
      {!groups.length && <EmptyState>暂无可见策略组</EmptyState>}
      <Modal open={history !== null} onClose={() => setHistory(null)} title={`${historyName} · 延迟历史`}>
        <div className="h-52">
          {history?.length ? <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={[...history].reverse()}>
              <CartesianGrid strokeDasharray="3 3" strokeOpacity={0.12} />
              <XAxis dataKey="testedAt" tickFormatter={v => new Date(v * 1000).toLocaleTimeString('zh-CN', {hour:'2-digit', minute:'2-digit'})} tick={{fontSize:10}} />
              <YAxis unit=" ms" tick={{fontSize:10}} width={65} />
              <Tooltip labelFormatter={v => formatUnixTime(Number(v))} formatter={(v, _name, item) => [item.payload.ok ? `${v} ms` : '超时/失败', '延迟']} />
              <Area type="monotone" dataKey="delayMs" name="延迟" stroke="#6366f1" fill="#6366f1" fillOpacity={0.15} />
            </AreaChart>
          </ResponsiveContainer> : <EmptyState>暂无测速记录</EmptyState>}
        </div>
        {!!history?.length && <div className="mt-3 max-h-48 space-y-1 overflow-auto text-xs">{history.slice(0, 10).map(r => <div key={r.id} className="flex justify-between border-b border-zinc-100 py-1.5 dark:border-zinc-800"><span className="text-zinc-500">{formatUnixTime(r.testedAt)}</span><span className={r.ok ? 'text-emerald-600' : 'text-red-500'}>{r.ok ? `${r.delayMs} ms` : '超时/失败'}</span></div>)}</div>}
      </Modal>
    </div>;
}
