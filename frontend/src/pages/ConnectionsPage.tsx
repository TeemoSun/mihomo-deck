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
  let chains: unknown = null;
  if (selected && 'chainJson' in selected) {
    try {
      chains = JSON.parse(selected.chainJson);
    } catch {
      chains = selected.chainJson;
    }
  }
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
        {selected && <div className="space-y-3 text-sm">
          <Badge>
  {selected.rule}
   · 
  {selected.rulePayload}
</Badge>
          {'metadata' in selected ? <>
            <pre className="overflow-auto whitespace-pre-wrap break-all text-xs">
              {JSON.stringify({
              ...selected.metadata,
              chains: selected.chains,
              providerChains: selected.providerChains,
              start: selected.start
            }, null, 2)}
            </pre>
            <Button onClick={() => setConfirm('one')}>断开此连接</Button>
          </> : <>
            <div>
  持续时间：
  {formatDuration(selected.durationMs)}
   · 关闭时间：
  {formatUnixTime(selected.closedAt)}
</div>
            <pre className="overflow-auto whitespace-pre-wrap break-all text-xs">
              {JSON.stringify({
              ...selected,
              chainJson: chains
            }, null, 2)}
            </pre>
          </>}
        </div>}
      </Modal>
      <ConfirmDialog open={!!confirm} onClose={() => setConfirm('')} title="确认操作" danger onConfirm={() => {
      if (confirm === 'all') run(api.closeConnections);
      if (confirm === 'history') run(api.clearConnections);
      if (confirm === 'one' && selected) run(() => api.closeConnection(selected.id));
    }}>此操作无法撤销，确定继续吗？</ConfirmDialog>
    </div>;
}
