export interface AuthUser { username: string; csrfToken: string }
export interface Preferences { theme: 'system'|'light'|'dark'; retentionDays: number; snapshotIntervalMin: number; logPersistLevel: string; hideInfoNodes: boolean; infoNodePattern: string; pinnedProxies: string[]; hiddenGroups: string[]; proxySort: string; delayTestUrl: string; delayTestTimeout: number; autoRefreshOverview: boolean; defaultConnectionsTab: string }
export interface TrafficSample { ts:number; upBps:number; downBps:number; memInuse:number; memOslimit:number; activeConns:number }
export interface TrafficPoint extends TrafficSample { upBytes:number; downBytes:number; peakUpBps:number; peakDownBps:number; avgMem:number; maxConns:number }
export interface TrafficSummary { cumulativeUp:number; cumulativeDown:number; todayUp:number; todayDown:number; weekUp:number; weekDown:number; monthUp:number; monthDown:number }
export interface StatItem { name:string; bytes:number; count:number }
export interface TopStatsResult { topHosts:StatItem[]; topProxies:StatItem[]; topSrcIps:StatItem[] }
export interface ClosedConnection { id:string; host:string; dstIp:string; dstPort:string; srcIp:string; srcPort:string; network:string; connType:string; inboundName:string; rule:string; rulePayload:string; chainJson:string; primaryProxy:string; providerName:string; uploadBytes:number; downloadBytes:number; startAt:number; closedAt:number; durationMs:number }
export interface PagedConnections { items:ClosedConnection[]; total:number; page:number; pageSize:number }
export interface LogEntry { id:number; ts:number; level:string; payload:string }
export interface PagedLogs { items:LogEntry[]; total:number; page:number; pageSize:number }
export interface DelayRecord { id:number; proxyName:string; groupName:string; testUrl:string; delayMs:number; ok:boolean; source:string; testedAt:number }
export interface ProviderSnapshot { id:number; providerName:string; uploadBytes:number; downloadBytes:number; totalBytes:number; expireTs:number; nodeCount:number; aliveCount:number; capturedAt:number }
export interface DBStats { fileSizeBytes:number; trafficSamples:number; trafficHourly:number; connectionHistory:number; logEntries:number; delayHistory:number; providerSnapshots:number }
export interface CollectorStatus { connected:boolean; lastErr:string; version:string; lastSeenAt:number }
export interface ActiveConnection { id:string; metadata:{ host:string; destinationIP:string; sniffHost:string; destinationPort:string; sourceIP:string; sourcePort:string; network:string; type:string; inboundName:string }; upload:number; download:number; start:string; chains:string[]; providerChains:string[]; rule:string; rulePayload:string }
export interface ConnectionsSnapshot { downloadTotal:number; uploadTotal:number; connections:ActiveConnection[]|null }
export interface ProxyItem { name:string; type:string; udp?:boolean; alive?:boolean; history?:{time:string; delay:number}[]; all?:string[]; now?:string; fixed?:string|boolean; 'provider-name'?:string }
export interface ProxyProvider { name:string; type:string; vehicleType?:string; updatedAt?:string; proxies:ProxyItem[]; subscriptionInfo?:{Upload:number; Download:number; Total:number; Expire:number} }
export interface RuleProvider { name:string; type:string; behavior:string; ruleCount:number; updatedAt:string; vehicleType:string }
export interface RuleItem { type:string; payload:string; proxy:string; size?:number; extra?: { disabled?: boolean; hitCount?: number; hitAt?: string; missCount?: number; missAt?: string } }
export interface MihomoConfig { mode:string; [key:string]:unknown }
export interface DNSQueryResponse { Status:number; Answer?:{name:string; type:number; TTL:number; data:string}[] }
export interface OverviewResponse { status:CollectorStatus; traffic:{up:number;down:number}; memory:{inuse:number;oslimit:number}; trafficSummary:TrafficSummary; trafficSeries:TrafficPoint[]; topStats:TopStatsResult; configs:MihomoConfig|null; providers:{providers:Record<string,ProxyProvider>}|null }
export interface LiveSnapshot { status:CollectorStatus; traffic:{up:number;down:number}; memory:{inuse:number;oslimit:number}; connections:ConnectionsSnapshot; trafficHistory:TrafficSample[]; liveTrafficHistory?:TrafficSample[]; logs:LogEntry[] }
export type LiveEvent = {type:'init';data:LiveSnapshot}|{type:'traffic';data:{up:number;down:number;ts?:number;sample?:TrafficSample}}|{type:'memory';data:{inuse:number;oslimit:number}}|{type:'connections';data:ConnectionsSnapshot}|{type:'log';data:LogEntry}|{type:'status';data:CollectorStatus}
