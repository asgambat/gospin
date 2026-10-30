// Ghostboard page: one shared text board per URL path, kept in sync through
// Server-Sent Events and saved back with debounced POST requests.

const GHOSTBOARD_SAVE_DEBOUNCE_MS = 300;
const GHOSTBOARD_BOARD_PREFIX = '/ui/ghostboard';
const GHOSTBOARD_BOARD_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;
const GHOSTBOARD_THEME_KEY = 'gosspin.ghostboard.theme';
const GHOSTBOARD_COPIED_MS = 1500;
const GHOSTBOARD_CLIENT_HEADER = 'X-Ghostboard-Client';
const GHOSTBOARD_PREVIEW_DEBOUNCE_MS = 150;
// Above this many characters the preview is not rendered: a board can hold
// megabytes of text, and re-parsing it on every keystroke would freeze the
// page. The text itself is still saved and editable.
const GHOSTBOARD_PREVIEW_MAX_CHARS = 262144;

// ghostboardEscapeHTML escapes the characters that start markup. The preview
// never accepts raw HTML: a board is user data, and injecting it into the page
// would run script with the session of the reader.
function ghostboardEscapeHTML(value) {
  return String(value).replace(/[&<>"']/g, (character) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  }[character]));
}

// ghostboardLinkTarget keeps a link from carrying a scheme that executes
// script; anything but http(s) and mailto is left as plain text.
function ghostboardLinkTarget(url) {
  const target = String(url).trim();
  return /^(https?:\/\/|mailto:)/i.test(target) ? target : '';
}

// ghostboardAutolinkURL matches a bare address in escaped text. Quotes and
// angle brackets arrive as entities, so the match is trimmed below.
const GHOSTBOARD_AUTOLINK_URL = /\bhttps?:\/\/[^\s]+/gi;
const GHOSTBOARD_AUTOLINK_EMAIL = /\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b/g;

// ghostboardAutolinkParts separates an address from the punctuation that
// belongs to the sentence around it, and from the entities the escaping left
// at its end, which are not part of the URL.
function ghostboardAutolinkParts(match) {
  let url = match;
  let trailing = '';

  let trimmed = true;
  while (trimmed) {
    trimmed = false;
    for (const entity of ['&quot;', '&#39;', '&gt;']) {
      if (url.endsWith(entity)) {
        url = url.slice(0, -entity.length);
        trailing = entity + trailing;
        trimmed = true;
      }
    }
  }

  const punctuation = url.match(/[.,;:!?]+$/);
  if (punctuation) {
    url = url.slice(0, -punctuation[0].length);
    trailing = punctuation[0] + trailing;
  }

  // A closing parenthesis without its opening one ends the sentence, as in
  // "see https://example.com/x_(y)" versus "(https://example.com)".
  let open = (url.match(/\(/g) || []).length;
  let close = (url.match(/\)/g) || []).length;
  while (url.endsWith(')') && close > open) {
    url = url.slice(0, -1);
    trailing = ')' + trailing;
    close--;
  }

  return { url: url, trailing: trailing };
}

// ghostboardInline renders the inline Markdown of an already escaped line.
function ghostboardInline(escapedLine) {
  // Every generated tag is lifted out as a placeholder, so the passes below
  // never touch the markup they just produced: a URL in a link label is not
  // turned into a nested anchor, and ** inside `code` is not bolded.
  const placeholders = [];
  const stash = (html) => {
    placeholders.push(html);
    return '\u0000' + (placeholders.length - 1) + '\u0000';
  };

  let output = escapedLine.replace(/`([^`\n]+)`/g, (match, code) => stash('<code>' + code + '</code>'));

  output = output.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (match, label, url) => {
    const target = ghostboardLinkTarget(url);
    if (!target) {
      // Not a link the page will follow: keep the literal Markdown instead of
      // silently dropping part of the user's text.
      return match;
    }
    return stash('<a href="' + target + '" target="_blank" rel="noopener noreferrer">' + label + '</a>');
  });

  output = output.replace(GHOSTBOARD_AUTOLINK_URL, (match) => {
    const parts = ghostboardAutolinkParts(match);
    const target = ghostboardLinkTarget(parts.url);
    if (!target) {
      return match;
    }
    return stash('<a href="' + target + '" target="_blank" rel="noopener noreferrer">' + target + '</a>') + parts.trailing;
  });

  output = output.replace(GHOSTBOARD_AUTOLINK_EMAIL, (match) =>
    stash('<a href="mailto:' + match + '">' + match + '</a>'));

  output = output.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  output = output.replace(/(^|[^*])\*([^*\n]+)\*/g, '$1<em>$2</em>');
  output = output.replace(/~~([^~]+)~~/g, '<del>$1</del>');

  // Restore from the last placeholder back: a link label can hold the
  // placeholder of a code span, whose index is lower.
  for (let index = placeholders.length - 1; index >= 0; index--) {
    output = output.split('\u0000' + index + '\u0000').join(placeholders[index]);
  }

  return output;
}

// ghostboardSplitRow splits a table row on the pipes that are not escaped with
// a backslash and not inside a code span, then trims the cells.
function ghostboardSplitRow(line) {
  let row = line.trim();
  if (row.startsWith('|')) {
    row = row.slice(1);
  }
  if (row.endsWith('|') && !row.endsWith('\\|')) {
    row = row.slice(0, -1);
  }

  const cells = [];
  let cell = '';
  let inCode = false;
  for (let index = 0; index < row.length; index++) {
    const character = row[index];
    if (character === '\\' && row[index + 1] === '|') {
      cell += '|';
      index++;
      continue;
    }
    if (character === '`') {
      inCode = !inCode;
    }
    if (character === '|' && !inCode) {
      cells.push(cell);
      cell = '';
      continue;
    }
    cell += character;
  }
  cells.push(cell);

  return cells.map((value) => value.trim());
}

// ghostboardTableSeparator returns the column alignments of a delimiter row, or
// null when the line is not one.
function ghostboardTableSeparator(line) {
  // A pipe is what separates a one-column delimiter row (| --- |) from a
  // horizontal rule (---), which is handled before this point.
  if (!line.includes('-') || !line.includes('|')) {
    return null;
  }

  const cells = ghostboardSplitRow(line);
  if (cells.length < 1) {
    return null;
  }

  const alignments = [];
  for (const cell of cells) {
    if (!/^:?-+:?$/.test(cell)) {
      return null;
    }
    const left = cell.startsWith(':');
    const right = cell.endsWith(':');
    alignments.push(left && right ? 'center' : right ? 'right' : left ? 'left' : '');
  }

  return alignments;
}

// ghostboardTable renders a header, its alignments and the body rows. A short
// row is padded and a long one is cut, like GitHub does.
function ghostboardTable(header, alignments, rows) {
  const cell = (tag, value, index) => {
    const align = alignments[index] ? ' class="gb-align-' + alignments[index] + '"' : '';
    return '<' + tag + align + '>' + ghostboardInline(ghostboardEscapeHTML(value)) + '</' + tag + '>';
  };

  const head = '<tr>' + header.map((value, index) => cell('th', value, index)).join('') + '</tr>';
  const body = rows
    .map((row) => '<tr>' + header.map((value, index) => cell('td', row[index] || '', index)).join('') + '</tr>')
    .join('');

  return '<table><thead>' + head + '</thead><tbody>' + body + '</tbody></table>';
}

// ghostboardMarkdown renders the supported Markdown subset: headings, fenced
// code, lists, blockquotes, rules, tables, paragraphs with soft line breaks, and
// bold, italic, strikethrough, inline code, links and bare URLs or addresses.
// Everything else stays literal text, which is the safe default for a shared
// board.
function ghostboardMarkdown(source) {
  const normalized = String(source === null || source === undefined ? '' : source)
    .replace(/\r\n?/g, '\n')
    // NUL is the code-span placeholder below, and the other controls would only
    // confuse the line parser.
    .replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f]/g, '');

  const blocks = [];
  let paragraph = [];
  let list = null;
  let quote = [];
  let code = null;

  const flushParagraph = () => {
    if (paragraph.length > 0) {
      blocks.push('<p>' + ghostboardInline(paragraph.map(ghostboardEscapeHTML).join('<br>')) + '</p>');
      paragraph = [];
    }
  };

  const flushList = () => {
    if (list !== null) {
      blocks.push('<' + list.tag + '>' + list.items
        .map((item) => '<li>' + ghostboardInline(ghostboardEscapeHTML(item)) + '</li>')
        .join('') + '</' + list.tag + '>');
      list = null;
    }
  };

  const flushQuote = () => {
    if (quote.length > 0) {
      blocks.push('<blockquote>' + ghostboardMarkdown(quote.join('\n')) + '</blockquote>');
      quote = [];
    }
  };

  const flushCode = () => {
    if (code !== null) {
      const language = code.language ? ' class="language-' + code.language + '"' : '';
      blocks.push('<pre><code' + language + '>' +
        code.lines.map(ghostboardEscapeHTML).join('\n') + '</code></pre>');
      code = null;
    }
  };

  const lines = normalized.split('\n');
  for (let index = 0; index < lines.length; index++) {
    const line = lines[index];

    if (code !== null) {
      if (/^\s*```\s*$/.test(line)) {
        flushCode();
      } else {
        code.lines.push(line);
      }
      continue;
    }

    const fence = line.match(/^\s*```\s*([A-Za-z0-9+#._-]{0,20})\s*$/);
    if (fence) {
      flushParagraph();
      flushList();
      flushQuote();
      code = { language: fence[1], lines: [] };
      continue;
    }

    if (/^\s*$/.test(line)) {
      flushParagraph();
      flushList();
      flushQuote();
      continue;
    }

    const heading = line.match(/^(#{1,6})\s+(.*)$/);
    if (heading) {
      flushParagraph();
      flushList();
      flushQuote();
      const level = heading[1].length;
      blocks.push('<h' + level + '>' + ghostboardInline(ghostboardEscapeHTML(heading[2])) + '</h' + level + '>');
      continue;
    }

    if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(line)) {
      flushParagraph();
      flushList();
      flushQuote();
      blocks.push('<hr>');
      continue;
    }

    if (line.includes('|') && index + 1 < lines.length) {
      const header = ghostboardSplitRow(line);
      const alignments = ghostboardTableSeparator(lines[index + 1]);
      if (header.length >= 1 && alignments !== null && alignments.length === header.length) {
        flushParagraph();
        flushList();
        flushQuote();

        const rows = [];
        let rowIndex = index + 2;
        while (rowIndex < lines.length && lines[rowIndex].trim() !== '' && lines[rowIndex].includes('|')) {
          rows.push(ghostboardSplitRow(lines[rowIndex]));
          rowIndex++;
        }

        blocks.push(ghostboardTable(header, alignments, rows));
        index = rowIndex - 1;
        continue;
      }
    }

    const unordered = line.match(/^\s*[-*+]\s+(.*)$/);
    const ordered = line.match(/^\s*\d+[.)]\s+(.*)$/);
    if (unordered || ordered) {
      flushParagraph();
      flushQuote();
      const tag = unordered ? 'ul' : 'ol';
      if (list === null || list.tag !== tag) {
        flushList();
        list = { tag: tag, items: [] };
      }
      list.items.push(unordered ? unordered[1] : ordered[1]);
      continue;
    }

    const quoted = line.match(/^\s*>\s?(.*)$/);
    if (quoted) {
      flushParagraph();
      flushList();
      quote.push(quoted[1]);
      continue;
    }

    flushList();
    flushQuote();
    paragraph.push(line);
  }

  // An unclosed fence still previews as code: while typing it is the expected
  // intermediate state.
  flushCode();
  flushParagraph();
  flushList();
  flushQuote();

  return blocks.join('');
}

// ghostboardTimestamp reads the updated_at of a board payload. The server sends
// the zero time for a board that was never written, and that has to read as "no
// time": rendering it would print a save time of year 1.
function ghostboardTimestamp(value) {
  if (!value) {
    return '';
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime()) || parsed.getUTCFullYear() <= 1) {
    return '';
  }
  return value;
}

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
    preview: false,
    previewHtml: '',
    previewTimer: null,
    files: [],
    uploads: [],
    filesOpen: false,
    filesError: '',
    dragging: false,
    confirmClear: false,
    maxFileBytes: 0,

    init() {
      this.clientId = this.createClientId();
      this.board = this.boardFromPath();
      this.boardValid = GHOSTBOARD_BOARD_PATTERN.test(this.board);
      this.shareUrl = this.buildShareUrl();
      this.theme = document.documentElement.classList.contains('gb-dark') ? 'dark' : 'light';
      // Zero means the server disabled attachments: the panel stays hidden.
      this.maxFileBytes = this.fileLimit();

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
      // The panel opens by itself when the board already carries files, and
      // stays as the user left it afterwards.
      this.filesOpen = this.files.length > 0;
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
      // A locked page must not keep the rendered text or the file list of the
      // session it lost.
      this.previewHtml = '';
      this.resetFiles();
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
      if (!this.updatedAt) {
        // A board can hold attachments without a line of text: it was written,
        // just not with text, so it is not empty.
        return this.files.length > 0 ? 'Files only, no text yet' : 'Nothing stored on this board yet';
      }
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

      // The server ends a private board's stream when its session expires and
      // says so with this event, so the page can bring the PIN form back
      // instead of showing a board that silently stopped updating.
      stream.addEventListener('locked', () => {
        this.sessionExpired('Your session expired: enter the PIN again to keep this board open.');
      });

      stream.onopen = () => {
        this.status = 'live';
      };

      stream.onerror = () => {
        // The expiry handler already closed the stream and showed the PIN form.
        if (this.locked) {
          return;
        }
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
        this.updatedAt = ghostboardTimestamp(payload.updated_at);
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
      this.updatedAt = ghostboardTimestamp(payload.updated_at);
      this.files = Array.isArray(payload.files) ? payload.files : [];
      this.pendingRemote = null;
      this.updatePreview();
    },

    onInput() {
      this.dirty = true;
      this.error = '';
      this.schedulePreview();
      clearTimeout(this.saveTimer);
      this.saveTimer = setTimeout(() => this.save(), GHOSTBOARD_SAVE_DEBOUNCE_MS);
    },

    onBlur() {
      if (this.dirty) {
        clearTimeout(this.saveTimer);
        this.save();
      }
    },

    // togglePreview swaps the textarea for the rendered board. The textarea
    // stays in the DOM (only hidden), so the caret logic and the file-size
    // dataset keep working when the user goes back to editing.
    togglePreview() {
      this.preview = !this.preview;
      if (this.preview) {
        this.updatePreview();
        this.$nextTick(() => {
          if (this.$refs.previewPane) {
            this.$refs.previewPane.focus();
          }
        });
        return;
      }

      this.$nextTick(() => {
        const area = document.getElementById('ghostboard-text');
        if (area) {
          area.focus();
        }
      });
    },

    // updatePreview renders the current text immediately. It runs only in
    // preview mode, so typing in edit mode costs nothing.
    updatePreview() {
      clearTimeout(this.previewTimer);
      this.previewTimer = null;

      if (!this.preview) {
        return;
      }

      if (this.text.length > GHOSTBOARD_PREVIEW_MAX_CHARS) {
        this.previewHtml = '<p class="gb-preview-empty">This board holds ' + this.text.length +
          ' characters, more than the ' + GHOSTBOARD_PREVIEW_MAX_CHARS +
          ' the preview renders. The text is saved as usual: switch to Edit to see it.</p>';
        return;
      }

      const rendered = ghostboardMarkdown(this.text);
      this.previewHtml = rendered ||
        '<p class="gb-preview-empty">Nothing on this board yet: switch to Edit and start typing.</p>';
    },

    // schedulePreview coalesces the renders of a burst of keystrokes.
    schedulePreview() {
      if (!this.preview) {
        return;
      }
      clearTimeout(this.previewTimer);
      this.previewTimer = setTimeout(() => this.updatePreview(), GHOSTBOARD_PREVIEW_DEBOUNCE_MS);
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
          this.sessionExpired('Your session expired: enter the PIN again to save this text.');
          return;
        }

        if (!response.ok) {
          throw new Error(await this.errorMessage(response));
        }

        const payload = await response.json();
        this.revision = payload.revision;
        this.updatedAt = ghostboardTimestamp(payload.updated_at);
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

    // sessionExpired brings the PIN form back after the session of an open
    // stream or of a save was rejected. The text typed locally is kept: it
    // belongs to the user, and unlocking writes it back.
    sessionExpired(message) {
      if (this.locked) {
        return;
      }
      this.locked = true;
      this.status = 'locked';
      this.lockError = message;
      this.error = '';
      this.pendingRemote = null;
      this.resetFiles();
      this.closeStream();
      this.focusPIN();
    },

    async errorMessage(response) {
      // A lockout carries how long the client must wait: say it instead of
      // leaving the user to retry blindly.
      if (response.status === 429) {
        try {
          const payload = await response.json();
          const seconds = Number(payload && payload.retry_after_secs);
          if (Number.isFinite(seconds) && seconds > 0) {
            return 'Too many attempts: try again in ' + seconds +
              (seconds === 1 ? ' second.' : ' seconds.');
          }
          if (payload && payload.error) {
            return payload.error + '.';
          }
        } catch (error) {
          // The body was not JSON; fall back to the generic message below.
        }
        return 'Too many attempts: wait a moment and try again.';
      }

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

      // The user picked a file to insert: show the editor so the insertion is
      // visible and its caret lands where expected.
      this.preview = false;
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

    get filesEnabled() {
      return this.maxFileBytes > 0;
    },

    get uploadingLabel() {
      const count = this.uploads.length;
      return count === 1 ? 'Uploading 1 file…' : 'Uploading ' + count + ' files…';
    },

    // fileLimit reads the attachment size limit the server injected, so the
    // page can refuse a file before sending it. Zero means attachments are off.
    fileLimit() {
      const maxBytes = document.body ? Number(document.body.dataset.maxFileBytes) : 0;
      return Number.isFinite(maxBytes) && maxBytes > 0 ? maxBytes : 0;
    },

    humanSize(bytes) {
      const value = Number(bytes);
      if (!Number.isFinite(value) || value < 0) {
        return '';
      }
      if (value < 1024) {
        return value + ' B';
      }
      const units = ['KiB', 'MiB', 'GiB', 'TiB'];
      let size = value / 1024;
      let unit = 0;
      while (size >= 1024 && unit < units.length - 1) {
        size /= 1024;
        unit++;
      }
      return (size >= 10 ? Math.round(size) : size.toFixed(1)) + ' ' + units[unit];
    },

    fileUrl(file) {
      return this.apiUrl() + '/files/' + encodeURIComponent(file.id);
    },

    // archiveUrl is every attachment of the board in one ZIP, so a board with
    // several files downloads in a single request.
    archiveUrl() {
      return this.apiUrl() + '/files.zip';
    },

    toggleFiles() {
      this.filesOpen = !this.filesOpen;
    },

    onFilesPicked(event) {
      const input = event.target;
      const files = input.files ? Array.from(input.files) : [];
      // Reset the input so picking the same file twice fires `change` again.
      input.value = '';
      this.uploadFiles(files);
    },

    onDrop(event) {
      this.dragging = false;
      const dropped = event.dataTransfer && event.dataTransfer.files;
      this.uploadFiles(dropped ? Array.from(dropped) : []);
    },

    // uploadFiles sends one file per request, one after the other: the server
    // accepts a single part per upload, and sequential requests keep the
    // progress of each file readable.
    async uploadFiles(files) {
      if (this.locked || !this.boardValid || files.length === 0) {
        return;
      }

      this.filesError = '';
      this.filesOpen = true;

      for (const file of files) {
        if (this.maxFileBytes > 0 && file.size > this.maxFileBytes) {
          this.filesError = '"' + file.name + '" is ' + this.humanSize(file.size) +
            ': this board accepts at most ' + this.humanSize(this.maxFileBytes) + ' per file.';
          continue;
        }
        if (file.size === 0) {
          this.filesError = '"' + file.name + '" is empty: there is nothing to upload.';
          continue;
        }
        await this.uploadOne(file);
      }
    },

    // uploadOne uses XMLHttpRequest rather than fetch because only XHR reports
    // upload progress and can abort a transfer in flight.
    uploadOne(file) {
      return new Promise((resolve) => {
        const upload = {
          id: 'up-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2),
          name: file.name,
          percent: 0,
          xhr: new XMLHttpRequest(),
        };
        this.uploads.push(upload);

        const xhr = upload.xhr;
        xhr.open('POST', this.apiUrl() + '/files');
        xhr.setRequestHeader(GHOSTBOARD_CLIENT_HEADER, this.clientId);

        if (xhr.upload) {
          xhr.upload.onprogress = (event) => {
            if (event.lengthComputable && event.total > 0) {
              // 100% is held back until the server has committed the file.
              upload.percent = Math.min(99, Math.round((event.loaded / event.total) * 100));
            }
          };
        }

        xhr.onload = () => {
          this.removeUpload(upload);

          if (xhr.status === 201) {
            // A session that expired while the transfer was in flight already
            // reset the page: do not repopulate the list behind the PIN form.
            if (this.locked) {
              resolve();
              return;
            }
            try {
              const stored = JSON.parse(xhr.responseText);
              this.files = [stored].concat(this.files.filter((item) => item.id !== stored.id));
              this.filesError = '';
            } catch (error) {
              console.warn('Ghostboard: invalid upload payload', error);
              this.filesError = '"' + file.name + '" was uploaded but the answer could not be read.';
            }
            resolve();
            return;
          }

          if (xhr.status === 401) {
            this.sessionExpired('Your session expired: enter the PIN again to upload files.');
            resolve();
            return;
          }

          this.filesError = this.uploadError(xhr, file);
          resolve();
        };

        xhr.onerror = () => {
          this.removeUpload(upload);
          this.filesError = 'Cannot upload "' + file.name + '": the server is unreachable.';
          resolve();
        };

        xhr.onabort = () => {
          this.removeUpload(upload);
          resolve();
        };

        const form = new FormData();
        form.append('file', file, file.name);
        xhr.send(form);
      });
    },

    // uploadError turns a rejection into a message that names the limit that
    // was hit, instead of repeating a status code.
    uploadError(xhr, file) {
      let payload = null;
      try {
        payload = JSON.parse(xhr.responseText);
      } catch (error) {
        payload = null;
      }
      const limit = payload ? payload.limit : '';

      if (xhr.status === 409) {
        return 'This board already holds ' + (payload && payload.limit_value ? payload.limit_value : 'its maximum of') +
          ' files: delete one before uploading another.';
      }
      if (xhr.status === 413) {
        if (limit === 'max_board_file_bytes') {
          return 'This board already holds its maximum of ' + this.humanSize(payload.limit_bytes) +
            ' of files: delete one before uploading another.';
        }
        if (limit === 'max_total_file_bytes') {
          return 'The server already holds its maximum of ' + this.humanSize(payload.limit_bytes) +
            ' of files: delete something before uploading.';
        }
        const allowed = payload && payload.limit_bytes ? payload.limit_bytes : this.maxFileBytes;
        return '"' + file.name + '" is too large: this board accepts at most ' + this.humanSize(allowed) + ' per file.';
      }
      if (xhr.status === 507) {
        return 'The server has no space left for files.';
      }
      if (xhr.status === 504) {
        return 'Uploading "' + file.name + '" took too long and was stopped.';
      }
      if (payload && payload.error) {
        return payload.error;
      }
      return 'Uploading "' + file.name + '" failed with status ' + xhr.status + '.';
    },

    removeUpload(upload) {
      const index = this.uploads.indexOf(upload);
      if (index >= 0) {
        this.uploads.splice(index, 1);
      }
    },

    cancelUpload(upload) {
      if (upload && upload.xhr) {
        upload.xhr.abort();
      }
    },

    // resetFiles drops what a page held when its session ended: the transfers
    // in flight, the list and the message about a previous failure.
    resetFiles() {
      for (const upload of this.uploads) {
        if (upload.xhr) {
          upload.xhr.abort();
        }
      }
      this.uploads = [];
      this.files = [];
      this.filesError = '';
      this.filesOpen = false;
    },

    async deleteFile(file) {
      if (!file || !file.id) {
        return;
      }
      if (!window.confirm('Delete "' + file.name + '" from this board?')) {
        return;
      }

      this.filesError = '';
      try {
        const response = await fetch(this.fileUrl(file), {
          method: 'DELETE',
          headers: { [GHOSTBOARD_CLIENT_HEADER]: this.clientId },
        });

        if (response.status === 401) {
          this.sessionExpired('Your session expired: enter the PIN again to delete files.');
          return;
        }
        if (!response.ok && response.status !== 404) {
          throw new Error(await this.errorMessage(response));
        }

        this.files = this.files.filter((item) => item.id !== file.id);
      } catch (error) {
        console.error('Ghostboard delete failed:', error);
        this.filesError = error.message || 'Cannot delete "' + file.name + '".';
      }
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

    // askClear opens the inline confirmation. Its two choices are separate on
    // purpose: clearing the text does not delete the attachments of a board,
    // and a board that holds only files needs the dialog too.
    askClear() {
      if (!this.text && this.files.length === 0) {
        return;
      }
      this.confirmClear = true;
    },

    clearTextOnly() {
      this.confirmClear = false;
      this.text = '';
      this.onInput();
      this.save();
    },

    async clearTextAndFiles() {
      this.confirmClear = false;
      this.filesError = '';

      try {
        const response = await fetch(this.apiUrl() + '/files', {
          method: 'DELETE',
          headers: { [GHOSTBOARD_CLIENT_HEADER]: this.clientId },
        });
        if (response.status === 401) {
          this.sessionExpired('Your session expired: enter the PIN again to clear this board.');
          return;
        }
        if (!response.ok && response.status !== 404) {
          throw new Error(await this.errorMessage(response));
        }
        this.files = [];
      } catch (error) {
        console.error('Ghostboard clear files failed:', error);
        this.filesError = error.message || 'Cannot delete the files of this board.';
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
