(() => {
  const key = 'mihomo-deck-theme'
  let theme = 'system'

  try {
    const stored = window.localStorage.getItem(key)
    if (stored === 'light' || stored === 'dark' || stored === 'system') {
      theme = stored
    }
  } catch {
    // Use the system preference when storage is unavailable.
  }

  const dark = theme === 'dark' || (
    theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches
  )
  document.documentElement.classList.toggle('dark', dark)
})()
