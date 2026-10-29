// Ghostboard page: one shared text board per URL path, kept in sync through
// Server-Sent Events and saved back with debounced POST requests.

const GHOSTBOARD_SAVE_DEBOUNCE_MS = 300;
const GHOSTBOARD_BOARD_PREFIX = '/ui/ghostboard';
const GHOSTBOARD_BOARD_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;
const GHOSTBOARD_THEME_KEY = 'gosspin.ghostboard.theme';
const GHOSTBOARD_COPIED_MS = 1500;
const GHOSTBOARD_CLIENT_HEADER = 'X-Ghostboard-Client';

function ghostboard() {
  return {
    board: 'default',
    boardValid: true,
    shareUrl: '',
    ready: false,
    locked: false,
    pin: '',
    lockError: '',
    unlocking: false,
    text: '',
    revision: 0,
    updatedAt: '',
    status: 'connecting',
    error: '',
    theme: 'light',
    dirty: false,
    saving: false,
    copied: false,
    copiedTimer: null,
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

      this.start();
    },

    // start loads the board state before opening the event stream. A private
    // board answers 401 without a session, so the page shows the PIN form
    // instead of a stream that would retry forever.
    async start() {
      this.status = 'connecting';
      this.error = '';

      let response;
      try {
        response = await fetch(this.apiUrl(), {
          headers: { Accept: 'application/json' },
          cache: 'no-store',
        });
      } catch (error) {
        console.error('Ghostboard: cannot reach the server', error);
        this.status = 'error';
        this.error = 'Cannot reach the server. Reload the page to try again.';
        return;
      }

      if (response.status === 401) {
        this.ready = true;
        this.showLock('');
        return;
      }
      if (!response.ok) {
        this.status = 'error';
        this.error = await this.errorMessage(response);
        return;
      }

      try {
        this.applyPayload(await response.json());
      } catch (error) {
        console.warn('Ghostboard: invalid board payload', error);
      }

      this.ready = true;
      this.locked = false;
      this.lockError = '';
      this.connect();
    },

    // showLock reveals the PIN form and drops what the page held, since a
    // locked page must not keep the text of a session that never started.
    showLock(message) {
      this.locked = true;
      this.status = 'locked';
      this.lockError = message || '';
      this.dirty = false;
      this.text = '';
      this.revision = 0;
      this.updatedAt = '';
      this.pendingRemote = null;
      this.closeStream();
      this.focusPIN();
    },

    focusPIN() {
      this.$nextTick(() => {
        if (this.$refs.pinInput) {
          this.$refs.pinInput.focus();
        }
      });
    },

    async unlock() {
      if (this.unlocking) {
        return;
      }

      const pin = this.pin.trim();
      if (!pin) {
        this.lockError = 'Enter the PIN of this board.';
        return;
      }

      this.unlocking = true;
      this.lockError = '';

      try {
        const response = await fetch(this.apiUrl() + '/unlock', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ pin }),
        });

        if (!response.ok) {
          throw new Error(await this.errorMessage(response));
        }

        this.pin = '';
        this.locked = false;

        // Text typed before the session expired is kept: it belongs to the
        // user, so it is written back instead of being replaced by the remote
        // state.
        if (this.dirty) {
          this.connect();
          this.save();
          return;
        }

        await this.start();
      } catch (error) {
        console.error('Ghostboard unlock failed:', error);
        this.lockError = error.message || 'Unlock failed.';
        this.pin = '';
        this.focusPIN();
      } finally {
        this.unlocking = false;
      }
    },

    get statusLabel() {
      switch (this.status) {
        case 'live': return 'Live';
        case 'connecting': return 'Connecting…';
        case 'reconnecting': return 'Reconnecting…';
        case 'locked': return 'Locked';
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
      if (this.locked) {
        return;
      }

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

        // The session expired while the page was open: show the PIN form and
        // keep the local text, so unlocking writes it back instead of losing
        // it. The textarea keeps its value while the form is hidden.
        if (response.status === 401) {
          this.locked = true;
          this.status = 'locked';
          this.lockError = 'Your session expired: enter the PIN again to save this text.';
          this.pendingRemote = null;
          this.closeStream();
          this.focusPIN();
          return;
        }

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
      // An insert can push the board over its limit. The text is kept, so say
      // what to do about it instead of only repeating the server text. Checked
      // before parsing the body so an unreadable error body cannot hide it.
      if (response.status === 413) {
        const limit = this.boardLimit();
        const size = limit > 0 ? ' of ' + limit + ' bytes' : '';
        return 'This board is over its size limit' + size +
          ': shorten the text, for example undo the last insert, and it will save again.';
      }

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
      if (!navigator.sendBeacon || this.locked) {
        return;
      }
      const payload = new Blob([JSON.stringify({ text: this.text })], { type: 'application/json' });
      navigator.sendBeacon(this.apiUrl(), payload);
    },

    async loadFile(event) {
      const input = event.target;
      const file = input.files && input.files[0];
      // Reset the input so picking the same file twice fires `change` again.
      input.value = '';

      if (!file || !this.boardValid) {
        return;
      }

      // The page carries the server-side board size, so an oversized file is
      // refused before it is read into memory.
      const maxBytes = this.boardLimit();
      if (maxBytes > 0 && file.size > maxBytes) {
        this.error = '"' + file.name + '" is ' + file.size +
          ' bytes: this board holds at most ' + maxBytes + ' bytes.';
        return;
      }

      let buffer;
      try {
        buffer = await file.arrayBuffer();
      } catch (error) {
        console.error('Ghostboard: cannot read the selected file', error);
        this.error = 'Cannot read "' + file.name + '".';
        return;
      }

      let content;
      try {
        // Boards hold text, so a file that is not valid UTF-8 is refused
        // instead of inserting replacement characters. TextDecoder drops a
        // leading BOM, like file.text() would.
        content = new TextDecoder('utf-8', { fatal: true }).decode(buffer);
      } catch (error) {
        console.warn('Ghostboard: the selected file is not valid UTF-8', error);
        this.error = '"' + file.name + '" is not valid UTF-8 text: save it as UTF-8 and try again.';
        return;
      }

      if (content === '') {
        this.error = '"' + file.name + '" is empty: nothing to insert.';
        return;
      }

      this.error = '';
      this.insertText(content);
      this.onInput();
      this.save();
    },

    insertText(content) {
      const area = document.getElementById('ghostboard-text');
      // The toolbar button keeps the focus in the textarea, so the caret (or
      // selection) is where the user wants the file; a board that never had
      // the focus gets the text appended at the end.
      const focused = area !== null && document.activeElement === area;
      const start = focused && typeof area.selectionStart === 'number' ? area.selectionStart : this.text.length;
      const end = focused && typeof area.selectionEnd === 'number' ? area.selectionEnd : start;

      this.text = this.text.slice(0, start) + content + this.text.slice(end);

      if (!area) {
        return;
      }

      // The caret lands right after the inserted text; wait one frame so the
      // DOM value already holds the new content.
      const caret = start + content.length;
      requestAnimationFrame(() => {
        area.focus();
        area.setSelectionRange(caret, caret);
      });
    },

    boardLimit() {
      const area = document.getElementById('ghostboard-text');
      const maxBytes = area ? Number(area.dataset.maxBytes) : 0;
      return Number.isFinite(maxBytes) && maxBytes > 0 ? maxBytes : 0;
    },

    async copy() {
      if (!this.text) {
        return;
      }

      try {
        await navigator.clipboard.writeText(this.text);
        this.markCopied();
        return;
      } catch (error) {
        // The clipboard API needs a secure context; fall back to a selection copy.
        console.warn('Ghostboard: clipboard API unavailable', error);
      }

      if (this.copyWithSelection()) {
        this.markCopied();
        return;
      }
      this.error = 'This browser does not allow copying to the clipboard.';
    },

    copyWithSelection() {
      const area = document.createElement('textarea');
      area.value = this.text;
      area.setAttribute('readonly', '');
      area.style.position = 'fixed';
      area.style.top = '0';
      area.style.left = '-9999px';
      document.body.appendChild(area);
      area.select();

      let copied = false;
      try {
        copied = document.execCommand('copy');
      } catch (error) {
        console.warn('Ghostboard: copy command failed', error);
      }
      area.remove();
      return copied;
    },

    markCopied() {
      this.copied = true;
      clearTimeout(this.copiedTimer);
      this.copiedTimer = setTimeout(() => {
        this.copied = false;
      }, GHOSTBOARD_COPIED_MS);
    },

    clearBoard() {
      if (!this.text) {
        return;
      }
      if (!window.confirm('Clear this board? Every connected device will lose its text.')) {
        return;
      }

      this.text = '';
      this.onInput();
      this.save();
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
