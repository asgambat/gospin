function homeApp() {
    return {
        data: null,
        stats: null,
        theme: 'auto',
        themes: ['auto','tokyo-night','catppuccin-latte','nord','dracula','gruvbox'],
        // Polling state
        _lastHash: null,
        _pollTimer: null,
        _statsPollTimer: null,
        _themeSaveTimer: null,
        _darkModeQuery: null,
        pageVisible: false,

        init() {
            // Load saved theme from localStorage
            const saved = localStorage.getItem('homepage-theme');
            if (saved && this.themes.includes(saved)) {
                this.theme = saved;
            }
            this.applyTheme(this.theme);
            // Listen for system color scheme changes (only relevant when theme === 'auto')
            this._darkModeQuery = window.matchMedia('(prefers-color-scheme: dark)');
            this._darkModeQuery.addEventListener('change', () => {
                if (this.theme === 'auto') this.applyTheme('auto');
            });
            this._requestStoragePersistence();
            this.fetchData();
            // Stats will start polling after fetchData loads the interval from settings
        },

        _requestStoragePersistence() {
            if (typeof navigator !== 'undefined' && navigator.storage && navigator.storage.persist) {
                navigator.storage.persist().then(granted => {
                    if (granted) {
                        console.log('Storage persistence granted — localStorage will not be evicted');
                    }
                }).catch(() => {
                    // Silently ignore if the API is not available or fails
                });
            }
        },

        applyTheme(themeName) {
            // Resolve 'auto' to the concrete system theme, but keep 'auto' as the logical value
            const displayTheme = themeName === 'auto' ? this._systemTheme() : this.normalizeTheme(themeName);
            document.body.className = document.body.className.replace(/theme-\S+/g, '');
            document.body.classList.add('theme-' + displayTheme);
            this.theme = themeName === 'auto' ? 'auto' : this.normalizeTheme(themeName);
        },

        _systemTheme() {
            return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'tokyo-night' : 'catppuccin-latte';
        },

        persistTheme(themeName) {
            this.applyTheme(themeName);
            // Debounce localStorage write — only persist the last theme after rapid changes
            if (this._themeSaveTimer) {
                clearTimeout(this._themeSaveTimer);
            }
            this._themeSaveTimer = setTimeout(() => {
                this._themeSaveTimer = null;
                localStorage.setItem('homepage-theme', themeName);
            }, 300);
        },

        normalizeTheme(name) {
            // Convert any format to lowercase kebab-case (e.g. "Catppuccin Latte" -> "catppuccin-latte")
            return name.toLowerCase().replace(/[\s_]+/g, '-').replace(/-+/g, '-').replace(/^-|-$/g, '');
        },

        async fetchData() {
            try {
                const resp = await this._fetchHomepageWithRetry();
                if (resp) this._loadData(resp);
            } catch (e) {
                console.warn('Failed to fetch homepage data after retries:', e);
            }
            if (!this.pageVisible) this.pageVisible = true;
        },

        async _fetchHomepageWithRetry() {
            const maxRetries = 2;
            let delay = 1000;
            for (let attempt = 0; attempt <= maxRetries; attempt++) {
                try {
                    const resp = await this._fetchHomepage();
                    if (resp) return resp;
                    // Server returned !res.ok — don't retry server errors
                    return null;
                } catch (e) {
                    if (attempt === maxRetries) throw e;
                    console.warn(`Fetch attempt ${attempt + 1} failed, retrying in ${delay}ms...`);
                    await new Promise(r => setTimeout(r, delay));
                    delay *= 2;
                }
            }
        },

        async _checkForConfigChanges() {
            try {
                const resp = await this._fetchHomepage();
                if (resp && resp.hash && resp.hash !== this._lastHash) {
                    console.log('Homepage config changed, reloading...');
                    this._loadData(resp);
                    this._reFade();
                }
            } catch (e) {
                // Silently ignore polling errors
            }
        },

        async _fetchHomepage() {
            const res = await fetch('/homepage');
            if (!res.ok) return null;
            return await res.json();
        },

        _loadData(resp) {
            if (resp.hash) {
                this._lastHash = resp.hash;
            }
            if (this.data) this._mergeIconFlags(this.data, resp);
            this.data = resp;
            // Sync font settings to <html> so Tailwind's rem-based classes scale correctly
            this._syncRootFont();
            if (this.data?.settings?.theme) {
                this.applyTheme(this.data.settings.theme);
            }
            this._startPolling();
            this._startStatsPolling();
        },

        _syncRootFont() {
            const root = document.documentElement.style;
            const fontSize = this.data?.settings?.fontSize || '17px';
            const fontFamily = this.data?.settings?.fontFamily
                || 'Inter, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif';
            root.fontSize = fontSize;
            root.fontFamily = fontFamily;
        },

        _startPolling() {
            // Clear existing timer
            if (this._pollTimer) {
                clearInterval(this._pollTimer);
                this._pollTimer = null;
            }

            // Determine polling interval from settings, fallback to default
            const intervalSeconds = this.data?.settings?.pollingIntervalSeconds;
            const interval = (intervalSeconds && intervalSeconds > 0) ? intervalSeconds * 1000 : 10000;

            // Start new polling timer
            this._pollTimer = setInterval(() => this._checkForConfigChanges(), interval);
        },

        _startStatsPolling() {
            // Clear existing timer
            if (this._statsPollTimer) {
                clearInterval(this._statsPollTimer);
                this._statsPollTimer = null;
            }

            // Fetch stats immediately
            this.fetchStats();

            // Determine polling interval from settings, fallback to default
            const intervalSeconds = this.data?.settings?.statsPollingIntervalSeconds;
            const interval = (intervalSeconds && intervalSeconds > 0) ? intervalSeconds * 1000 : 3000;

            // Start new polling timer
            this._statsPollTimer = setInterval(() => this.fetchStats(), interval);
        },

        _mergeIconFlags(oldData, newData) {
            // Build a map of service name → icon flags from the old data
            const flags = {};
            if (oldData.services) {
                for (const group of oldData.services) {
                    if (group.items) {
                        for (const svc of group.items) {
                            if (svc._iconLoaded !== undefined || svc._iconFailed !== undefined) {
                                flags[svc.name] = {
                                    _iconLoaded: svc._iconLoaded,
                                    _iconFailed: svc._iconFailed
                                };
                            }
                        }
                    }
                }
            }
            // Apply preserved flags to matching services in the new data
            if (newData.services) {
                for (const group of newData.services) {
                    if (group.items) {
                        for (const svc of group.items) {
                            if (flags[svc.name]) {
                                svc._iconLoaded = flags[svc.name]._iconLoaded;
                                svc._iconFailed = flags[svc.name]._iconFailed;
                            }
                        }
                    }
                }
            }
        },

        _reFade() {
            this.pageVisible = false;
            const el = document.getElementById('app');
            const onEnd = () => {
                el.removeEventListener('transitionend', onEnd);
                this.pageVisible = true;
            };
            el.addEventListener('transitionend', onEnd);
        },

        async fetchStats() {
            try {
                const resp = await this._fetchStats();
                if (resp) this.stats = resp;
            } catch (e) {
                console.warn('Failed to fetch system stats:', e);
            }
        },

        async _fetchStats() {
            const res = await fetch('/runtime/system-stats');
            if (!res.ok) return null;
            return await res.json();
        }
    };
}
