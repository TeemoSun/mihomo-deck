import { useEffect, useState } from 'react';
import { api } from '../api/client';
import { Badge, Button, Card, ConfirmDialog, EmptyState, Input, Tabs } from '../components/ui';
import { formatUnixTime } from '../lib/format';
import { usePrefsStore } from '../stores/usePrefsStore';
import { useLiveStore } from '../stores/useLiveStore';
import { useToastStore } from '../stores/useToastStore';
import type { PagedLogs } from '../types/api';
const levels = ['all', 'error', 'warning', 'info', 'debug'];
export default function LogsPage() {
  const [tab, setTab] = useState('live');
  const [level, setLevel] = useState('all');
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(1);
  const [history, setHistory] = useState<PagedLogs | null>(null);
  const [confirm, setConfirm] = useState(false);
  const [revision, setRevision] = useState(0);
  const {
    logs,
    logsPaused,
    setLogsPaused
  } = useLiveStore();
  const toast = useToastStore();
  const prefs = usePrefsStore(s => s.prefs);
  useEffect(() => {
    if (tab !== 'history') return;
    void api.logs({
      level: level === 'all' ? undefined : level,
      q: query,
      page,
      pageSize: 50
    }).then(setHistory).catch(() => toast.error('加载历史日志失败'));
  }, [tab, level, query, page, revision]);
  const entries = tab === 'live' ? [...logs].reverse().filter(l => (level === 'all' || l.level.toLowerCase() === level) && l.payload.toLowerCase().includes(query.toLowerCase())) : history?.items || [];
  return <div className="space-y-4">
      <h1 className="text-xl font-semibold">运行日志</h1>
      <Tabs value={tab} onChange={v => {
      setTab(v);
      setPage(1);
    }} items={[{
      value: 'live',
      label: '实时日志'
    }, {
      value: 'history',
      label: '历史日志'
    }]} />
      <div className="flex flex-wrap gap-2">
        {levels.map(l => <Button key={l} className={level === l ? '!border-indigo-500 !text-indigo-600' : ''} onClick={() => {
        setLevel(l);
        setPage(1);
      }}>
          {l === 'all' ? '全部' : l}
        </Button>)}
      </div>
      <div className="flex flex-wrap gap-2">
        <Input className="min-w-48 flex-1" placeholder="搜索日志" value={query} onChange={e => {
        setQuery(e.target.value);
        setPage(1);
      }} />
        {tab === 'live' ? <Button onClick={() => setLogsPaused(!logsPaused)}>
          {logsPaused ? '恢复日志' : '暂停日志'}
        </Button> : <Button onClick={() => setConfirm(true)}>清空历史日志</Button>}
      </div>
      {tab === 'live' && level === 'debug' && prefs?.logPersistLevel !== 'debug' && <div className="rounded-md border border-indigo-200 bg-indigo-50 px-3 py-2 text-sm text-indigo-700 dark:border-indigo-900 dark:bg-indigo-950/40 dark:text-indigo-300">查看 debug 日志需要在内核设置中将“日志持久化级别”设为 debug。</div>}
      <Card className="overflow-hidden">
        {entries.map((entry, index) => <div key={`${entry.id}-${index}`} className="flex items-start gap-2 border-b border-zinc-100 p-3 text-xs dark:border-zinc-800">
          <span className="whitespace-nowrap font-mono text-zinc-500">
            {formatUnixTime(entry.ts)}
          </span>
          <Badge className={entry.level === 'error' ? '!text-red-600' : entry.level === 'warning' ? '!text-amber-600' : '!text-indigo-500'}>
            {entry.level}
          </Badge>
          <span className="min-w-0 flex-1 whitespace-pre-wrap break-all font-mono">
            {entry.payload}
          </span>
          <Button onClick={() => void navigator.clipboard.writeText(entry.payload).then(() => toast.success('已复制')).catch(() => toast.error('复制失败'))}>复制</Button>
        </div>)}
        {!entries.length && <EmptyState>暂无日志</EmptyState>}
      </Card>
      {tab === 'history' && <div className="flex justify-end gap-2 text-sm">
        <span>
  共 
  {history?.total || 0}
   条 · 第 
  {page}
   页
</span>
        <Button disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</Button>
        <Button disabled={page * 50 >= (history?.total || 0)} onClick={() => setPage(page + 1)}>下一页</Button>
      </div>}
      <ConfirmDialog open={confirm} onClose={() => setConfirm(false)} title="清空历史日志" danger onConfirm={() => void api.clearLogs().then(() => {
      setPage(1);
      setRevision(v => v + 1);
      toast.success('已清空');
    }).catch(() => toast.error('清空失败'))}>确定删除所有历史日志吗？</ConfirmDialog>
    </div>;
}
