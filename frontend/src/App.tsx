import { useEffect } from 'react'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { useAuthStore } from './stores/useAuthStore'
import { ToastContainer } from './components/ui'
import AppShell from './components/layout/AppShell'
import LoginPage from './pages/LoginPage'
import OverviewPage from './pages/OverviewPage'
import ProxiesPage from './pages/ProxiesPage'
import ConnectionsPage from './pages/ConnectionsPage'
import ProvidersPage from './pages/ProvidersPage'
import LogsPage from './pages/LogsPage'
import SettingsPage from './pages/SettingsPage'

export default function App() {
  const authenticated = useAuthStore(s => s.user !== null)
  const checking = useAuthStore(s => s.checking)
  const checkAuth = useAuthStore(s => s.checkAuth)
  useEffect(() => { void checkAuth() }, [checkAuth])
  return (
    <>
      <ToastContainer />
      {checking ? <div className="p-8 text-sm text-zinc-500">正在检查登录状态…</div> : !authenticated ? <LoginPage /> : (
        <BrowserRouter>
          <Routes>
            <Route element={<AppShell />}>
              <Route index element={<OverviewPage />} />
              <Route path="proxies" element={<ProxiesPage />} />
              <Route path="connections" element={<ConnectionsPage />} />
              <Route path="providers" element={<ProvidersPage />} />
              <Route path="logs" element={<LogsPage />} />
              <Route path="settings" element={<SettingsPage />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Route>
          </Routes>
        </BrowserRouter>
      )}
    </>
  )
}
