// Ghostboard page: one shared text board per URL path, kept in sync through
// Server-Sent Events and saved back with debounced POST requests.

const GHOSTBOARD_SAVE_DEBOUNCE_MS = 300;
const GHOSTBOARD_BOARD_PREFIX = '/ui/ghostboard';
const GHOSTBOARD_BOARD_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;
const GHOSTBOARD_THEME_KEY = 'gosspin.ghostboard.theme';
const GHOSTBOARD_CLIENT_HEADER = 'X-Ghostboard-Client';

function ghostboard() {
  return {
    board: 'default',
    boardValid: true,
    shareUrl: '',
    text: '',
    revision: 0,
    updatedAt: '',
    status: 'connecting',
    error: '',
    theme: 'light',
    dirty: false,
    saving: false,
    pendingRemote: null,
    saveTimer: null,
    clientId: '',
    stream: null,

    init() {
      this.clientId = this.createClientId();
      this.board = this.boardFromPath();
      this.boardValid = GHOSTBOARD_BOARD_PATTERN.test(this.board);
      this.shareUrl = this.buildShareUrl();
      this.theme = document.documentElement.classList.contains('gb-dark') ? 'dark' : 'light';

      if (!this.boardValid) {
        this.status = 'error';
        this.error = 'Invalid board name: use 1-64 letters, digits, dots, dashes or underscores.';
        return;
      }

      // Flush pending text if the tab is closed before the debounce fires.
      window.addEventListener('pagehide', () => {
        if (this.dirty) {
          this.sendBeacon();
        }
      });

      this.connect();
    },

    get statusLabel() {
      switch (this.status) {
        case 'live': return 'Live';
        case 'connecting': return 'Connecting…';
        case 'reconnecting': return 'Reconnecting…';
        case 'error': return 'Offline';
        default: return this.status;
      }
    },

    get statusClass() {
      return 'gb-pill-' + this.status;
    },

    get revisionLabel() {
      return this.revision ? 'revision ' + this.revision : 'not saved yet';
    },

    get saveLabel() {
      if (this.saving) return 'Saving…';
      if (this.dirty) return 'Unsaved changes';
      if (!this.updatedAt) return 'Nothing stored on this board yet';
      return 'Saved at ' + new Date(this.updatedAt).toLocaleTimeString();
    },

    apiUrl() {
      return '/ghostboard/' + encodeURIComponent(this.board);
    },

    connect() {
      this.closeStream();
      this.status = 'connecting';

      const stream = new EventSource(this.apiUrl() + '/events');
      this.stream = stream;

      stream.addEventListener('board', (event) => {
        let payload;
        try {
          payload = JSON.parse(event.data);
        } catch (error) {
          console.warn('Ghostboard: invalid event payload', error);
          return;
        }
        this.applyRemote(payload);
      });

      stream.onopen = () => {
        this.status = 'live';
      };

      stream.onerror = () => {
        if (stream.readyState === EventSource.CLOSED) {
          this.status = 'error';
          this.error = 'Connection lost. Reload the page to reconnect.';
          return;
        }
        this.status = 'reconnecting';
      };
    },

    closeStream() {
      if (this.stream) {
        this.stream.close();
        this.stream = null;
      }
    },

    applyRemote(payload) {
      if (!payload || typeof payload.text !== 'string') {
        return;
      }

      // Ignore the echo of our own write; it would move the caret while typing.
      if (payload.client_id && payload.client_id === this.clientId) {
        this.revision = payload.revision;
        this.updatedAt = payload.updated_at;
        return;
      }

      if (this.dirty) {
        this.pendingRemote = payload;
        return;
      }

      this.applyPayload(payload);
    },

    applyPayload(payload) {
      this.text = payload.text;
      this.revision = payload.revision;
      this.updatedAt = payload.updated_at;
      this.pendingRemote = null;
    },

    onInput() {
      this.dirty = true;
      this.error = '';
      clearTimeout(this.saveTimer);
      this.saveTimer = setTimeout(() => this.save(), GHOSTBOARD_SAVE_DEBOUNCE_MS);
    },

    onBlur() {
      if (this.dirty) {
        clearTimeout(this.saveTimer);
        this.save();
      }
    },

    async save() {
      clearTimeout(this.saveTimer);
      this.saveTimer = null;

      if (!this.boardValid || !this.dirty) {
        return;
      }
      if (this.saving) {
        this.saveTimer = setTimeout(() => this.save(), GHOSTBOARD_SAVE_DEBOUNCE_MS);
        return;
      }

      const text = this.text;
      this.saving = true;

      try {
        const response = await fetch(this.apiUrl(), {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            [GHOSTBOARD_CLIENT_HEADER]: this.clientId,
          },
          body: JSON.stringify({ text }),
        });

        if (!response.ok) {
          throw new Error(await this.errorMessage(response));
        }

        const payload = await response.json();
        this.revision = payload.revision;
        this.updatedAt = payload.updated_at;
        // Clear a banner left by a previous failed save once a retry succeeds.
        this.error = '';

        if (this.text === text) {
          this.dirty = false;
        }

        // A remote update deferred while typing is only applied if it is
        // newer than the revision we just wrote; otherwise it is stale.
        if (!this.dirty && this.pendingRemote) {
          const pending = this.pendingRemote;
          this.pendingRemote = null;
          if (pending.revision > this.revision) {
            this.applyPayload(pending);
          }
        }

        // Text typed while the request was in flight needs another round trip.
        if (this.dirty) {
          this.saveTimer = setTimeout(() => this.save(), GHOSTBOARD_SAVE_DEBOUNCE_MS);
        }
      } catch (error) {
        console.error('Ghostboard save failed:', error);
        this.error = error.message || 'Failed to save the board.';
      } finally {
        this.saving = false;
      }
    },

    async errorMessage(response) {
      try {
        const payload = await response.json();
        if (payload && payload.error) {
          return payload.error;
        }
      } catch (error) {
        // The body was not JSON; fall back to the status code.
      }
      return 'Request failed with status ' + response.status;
    },

    sendBeacon() {
      if (!navigator.sendBeacon) {
        return;
      }
      const payload = new Blob([JSON.stringify({ text: this.text })], { type: 'application/json' });
      navigator.sendBeacon(this.apiUrl(), payload);
    },

    download() {
      const blob = new Blob([this.text], { type: 'text/plain;charset=utf-8' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = (this.board === 'default' ? 'ghostboard' : this.board) + '.txt';
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(url);
    },

    toggleTheme() {
      const dark = document.documentElement.classList.toggle('gb-dark');
      this.theme = dark ? 'dark' : 'light';
      try {
        localStorage.setItem(GHOSTBOARD_THEME_KEY, this.theme);
      } catch (error) {
        console.warn('Ghostboard: cannot persist the theme', error);
      }
    },

    boardFromPath() {
      const path = window.location.pathname;
      if (!path.startsWith(GHOSTBOARD_BOARD_PREFIX)) {
        return 'default';
      }

      const rest = path.slice(GHOSTBOARD_BOARD_PREFIX.length).replace(/^\/+|\/+$/g, '');
      if (!rest) {
        return 'default';
      }

      try {
        return decodeURIComponent(rest);
      } catch (error) {
        return rest;
      }
    },

    buildShareUrl() {
      const suffix = this.board === 'default' ? '' : '/' + encodeURIComponent(this.board);
      return window.location.origin + GHOSTBOARD_BOARD_PREFIX + suffix;
    },

    createClientId() {
      if (window.crypto && typeof window.crypto.randomUUID === 'function') {
        return window.crypto.randomUUID();
      }
      return 'tab-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2);
    },
  };
}
