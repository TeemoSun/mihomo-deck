import { useEffect, useState } from 'react';
import { api } from '../api/client';
import { Badge, Button, Card, ConfirmDialog, EmptyState, Input, Modal, Select, Tabs } from '../components/ui';
import { formatBytes, formatDuration, formatUnixTime } from '../lib/format';
import { useLiveStore } from '../stores/useLiveStore';
import { usePrefsStore } from '../stores/usePrefsStore';
import { useToastStore } from '../stores/useToastStore';
import type { ActiveConnection, ClosedConnection, PagedConnections } from '../types/api';
export default function ConnectionsPage() {
  const prefs = usePrefsStore(s => s.prefs);
  const live = useLiveStore(s => s.connections);
  const toast = useToastStore();
  const [tab, setTab] = useState(prefs?.defaultConnectionsTab === 'history' ? 'history' : 'active');
  const [initialized, setInitialized] = useState(!!prefs);
  useEffect(() => {
    if (prefs && !initialized) {
      setTab(prefs.defaultConnectionsTab === 'history' ? 'history' : 'active');
      setInitialized(true);
    }
  }, [prefs, initialized]);
  const [paused, setPaused] = useState(false);
  const [frozen, setFrozen] = useState<ActiveConnection[]>([]);
  const [query, setQuery] = useState('');
  const [range, setRange] = useState('all');
  const [page, setPage] = useState(1);
  const [history, setHistory] = useState<PagedConnections | null>(null);
  const [selected, setSelected] = useState<ActiveConnection | ClosedConnection | null>(null);
  const [confirm, setConfirm] = useState('');
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    if (tab !== 'history') return;
    const seconds = Number(range);
    void api.connections({
      q: query,
      from: seconds ? Math.floor(Date.now() / 1000) - seconds : undefined,
      page,
      pageSize: 30
    }).then(setHistory).catch(() => toast.error('加载历史连接失败'));
  }, [tab, query, range, page, revision]);
  const entries = (paused ? frozen : live).filter(c => JSON.stringify([c.metadata.host, c.metadata.destinationIP, c.metadata.sourceIP, c.chains, c.rule]).toLowerCase().includes(query.toLowerCase()));
  const run = (fn: () => Promise<void>) => void fn().then(() => {
    toast.success('操作完成');
    setRevision(v => v + 1);
    setSelected(null);
  }).catch(() => toast.error('操作失败'));
  const detail = selected ? (() => {
    if ('metadata' in selected) return {host:selected.metadata.host, dst:`${selected.metadata.destinationIP}:${selected.metadata.destinationPort}`, src:`${selected.metadata.sourceIP}:${selected.metadata.sourcePort}`, network:`${selected.metadata.network} / ${selected.metadata.type}`, inbound:selected.metadata.inboundName, chains:selected.chains, up:selected.upload, down:selected.download, start:selected.start ? new Date(selected.start).toLocaleString('zh-CN') : '—', closed:'—', duration:selected.start ? formatDuration(Math.max(0, Date.now() - new Date(selected.start).getTime())) : '—'};
    let chains: string[] = []; try { const parsed: unknown = JSON.parse(selected.chainJson); if (Array.isArray(parsed)) chains = parsed.map(String); } catch { /* 无法解析代理链 */ }
    return {host:selected.host, dst:`${selected.dstIp}:${selected.dstPort}`, src:`${selected.srcIp}:${selected.srcPort}`, network:`${selected.network} / ${selected.connType}`, inbound:selected.inboundName, chains, up:selected.uploadBytes, down:selected.downloadBytes, start:formatUnixTime(selected.startAt), closed:formatUnixTime(selected.closedAt), duration:formatDuration(selected.durationMs)};
  })() : null;
  return <div className="space-y-4">
      <h1 className="text-xl font-semibold">连接审计</h1>
      <Tabs value={tab} onChange={v => {
      setTab(v);
      setPage(1);
      setQuery('');
    }} items={[{
      value: 'active',
      label: '实时活跃连接'
    }, {
      value: 'history',
      label: '历史已关闭连接'
    }]} />
      <div className="flex flex-wrap gap-2">
        <Input className="min-w-48 flex-1" placeholder="搜索连接" value={query} onChange={e => {
        setQuery(e.target.value);
        setPage(1);
      }} />
        {tab === 'active' ? <>
          <Button onClick={() => {
          if (!paused) setFrozen(live);
          setPaused(!paused);
        }}>
            {paused ? '恢复更新' : '暂停更新'}
          </Button>
          <Button onClick={() => setConfirm('all')}>全部断开</Button>
        </> : <>
          <Select value={range} onChange={e => {
          setRange(e.target.value);
          setPage(1);
        }}>
            <option value="all">全部时间</option>
            <option value="3600">1小时</option>
            <option value="86400">24小时</option>
            <option value="604800">7天</option>
            <option value="2592000">30天</option>
          </Select>
          <Button onClick={() => setConfirm('history')}>清空历史连接</Button>
        </>}
      </div>
      {tab === 'active' && <div className="text-sm text-zinc-500">
  活跃 
  {entries.length}
   / 
  {(paused ? frozen : live).length}
   · 上传 
  {formatBytes((paused ? frozen : live).reduce((n, c) => n + c.upload, 0))}
   · 下载 
  {formatBytes((paused ? frozen : live).reduce((n, c) => n + c.download, 0))}
</div>}
      <Card className="overflow-hidden">
        <div className="hidden grid-cols-4 gap-3 border-b p-3 text-xs text-zinc-500 md:grid">
          <span>目标</span>
          <span>来源</span>
          <span>策略 / 规则</span>
          <span>上传 / 下载</span>
        </div>
        {(tab === 'active' ? entries : history?.items || []).map(c => {
        const active = 'metadata' in c;
        return <button key={c.id} onClick={() => setSelected(c)} className="grid w-full gap-1 border-b border-zinc-100 p-3 text-left text-sm hover:bg-zinc-50 dark:border-zinc-800 dark:hover:bg-zinc-800 md:grid-cols-4 md:gap-3">
          <span className="break-all font-medium">
            {active ? c.metadata.host || c.metadata.destinationIP : c.host || c.dstIp}
          </span>
          <span className="text-zinc-500">
            {active ? c.metadata.sourceIP : c.srcIp}
          </span>
          <span className="truncate text-zinc-500">
  {active ? c.chains?.join(' → ') : c.primaryProxy}
   · 
  {c.rule}
</span>
          <span className="text-zinc-500">
  ↑ 
  {formatBytes(active ? c.upload : c.uploadBytes)}
   / ↓ 
  {formatBytes(active ? c.download : c.downloadBytes)}
</span>
        </button>;
      })}
        {(tab === 'active' ? entries.length === 0 : !history?.items?.length) && <EmptyState>暂无连接</EmptyState>}
      </Card>
      {tab === 'history' && <div className="flex items-center justify-end gap-3 text-sm">
        <span>
  共 
  {history?.total || 0}
   条 · 第 
  {page}
   页
</span>
        <Button disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</Button>
        <Button disabled={page * 30 >= (history?.total || 0)} onClick={() => setPage(page + 1)}>下一页</Button>
      </div>}
      <Modal open={!!selected} onClose={() => setSelected(null)} title="连接详情">
        {selected && detail && <div className="space-y-4 text-sm">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">{([['目标主机', detail.host], ['目标 IP:端口', detail.dst], ['来源 IP:端口', detail.src], ['网络/协议', detail.network], ['入站', detail.inbound], ['命中规则与载荷', `${selected.rule} · ${selected.rulePayload}`], ['上传/下载流量', `↑ ${formatBytes(detail.up)} · ↓ ${formatBytes(detail.down)}`], ['开始时间', detail.start], ['关闭时间', detail.closed], ['持续时长', detail.duration]]).map(([label,value]) => <div key={label} className="min-w-0"><div className="text-xs text-zinc-500">{label}</div><div className="mt-1 break-all">{value || '—'}</div></div>)}</div>
          <div><div className="mb-1 text-xs text-zinc-500">完整代理链</div><div className="flex flex-wrap items-center gap-1">{detail.chains.length ? detail.chains.map((chain, i) => <span key={i} className="flex items-center gap-1">{i > 0 && '→'}<Badge>{chain}</Badge></span>) : '—'}</div></div>
          <details><summary className="cursor-pointer text-zinc-500">原始 JSON</summary><pre className="mt-2 overflow-auto whitespace-pre-wrap break-all text-xs">{JSON.stringify(selected, null, 2)}</pre></details>
          {'metadata' in selected && <Button onClick={() => setConfirm('one')}>断开此连接</Button>}
        </div>}
      </Modal>
      <ConfirmDialog open={!!confirm} onClose={() => setConfirm('')} title="确认操作" danger onConfirm={() => {
      if (confirm === 'all') run(api.closeConnections);
      if (confirm === 'history') run(api.clearConnections);
      if (confirm === 'one' && selected) run(async () => { await api.closeConnection(selected.id); if (paused) setFrozen(items => items.filter(c => c.id !== selected.id)); });
    }}>此操作无法撤销，确定继续吗？</ConfirmDialog>
    </div>;
}
