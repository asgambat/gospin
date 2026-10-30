// sealed.js implements the sealed board page: it mints the key, encrypts the
// text with WebCrypto before writing and decrypts the envelope after reading, so
// the server never sees the plaintext or the key.
//
// The envelope is the one of internal/sealedcrypto:
//
//	{"v":1,"iv":"<base64url>","ct":"<base64url>"}
//
// AES-256-GCM, a fresh 96-bit IV per write and the board name as additional
// authenticated data. ui/assets/sealed-vector.json pins this implementation to
// the Go one; window.sealedSelfTest() checks it against that file and is what the
// end-to-end pass drives from the browser.
//
// Nothing here stores the key: no localStorage, no sessionStorage, no cookie. The
// key lives in the fragment of the address bar while the page is open.
(function () {
    'use strict';

    var AAD_PREFIX = 'gospin-sealed-v1|';
    var ENVELOPE_VERSION = 1;
    var KEY_BYTES = 32;
    var IV_BYTES = 12;
    var TAG_BITS = 128;
    var BOARD_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;
    var VECTOR_PATH = '/ui/assets/sealed-vector.json';

    function isSupported() {
        return window.isSecureContext === true && !!(window.crypto && window.crypto.subtle);
    }

    function base64urlEncode(bytes) {
        var binary = '';
        for (var i = 0; i < bytes.length; i++) {
            binary += String.fromCharCode(bytes[i]);
        }
        return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    }

    function base64urlDecode(text) {
        if (typeof text !== 'string' || text.length === 0) {
            return null;
        }
        if (!/^[A-Za-z0-9_-]+$/.test(text)) {
            return null;
        }
        var padded = text.replace(/-/g, '+').replace(/_/g, '/');
        while (padded.length % 4 !== 0) {
            padded += '=';
        }
        try {
            var binary = atob(padded);
            var bytes = new Uint8Array(binary.length);
            for (var i = 0; i < binary.length; i++) {
                bytes[i] = binary.charCodeAt(i);
            }
            return bytes;
        } catch (error) {
            return null;
        }
    }

    function hexDecode(text) {
        if (!/^[0-9a-fA-F]{64}$/.test(text)) {
            return null;
        }
        var bytes = new Uint8Array(KEY_BYTES);
        for (var i = 0; i < KEY_BYTES; i++) {
            bytes[i] = parseInt(text.substr(i * 2, 2), 16);
        }
        return bytes;
    }

    // parseKey reads a key from the accepted forms: base64url, hex and a whole
    // sealed board link whose fragment carries k=<key>.
    function parseKey(text) {
        var value = String(text || '').trim();
        var hash = value.indexOf('#');
        if (hash >= 0) {
            value = value.slice(hash + 1);
        }
        if (value.indexOf('k=') === 0) {
            value = value.slice(2);
        }
        if (!value) {
            return null;
        }
        var hex = hexDecode(value);
        if (hex) {
            return hex;
        }
        var raw = base64urlDecode(value);
        if (!raw || raw.length !== KEY_BYTES) {
            return null;
        }
        return raw;
    }

    function newKey() {
        return window.crypto.getRandomValues(new Uint8Array(KEY_BYTES));
    }

    async function importKey(raw) {
        return window.crypto.subtle.importKey('raw', raw, { name: 'AES-GCM' }, false, ['encrypt', 'decrypt']);
    }

    // sealWithIV encrypts with a caller-supplied IV. It exists for the known
    // answer vector, which has to reproduce a published envelope byte for byte.
    async function sealWithIV(keyBytes, board, plaintext, iv) {
        var key = await importKey(keyBytes);
        var ciphertext = await window.crypto.subtle.encrypt({
            name: 'AES-GCM',
            iv: iv,
            additionalData: new TextEncoder().encode(AAD_PREFIX + board),
            tagLength: TAG_BITS
        }, key, new TextEncoder().encode(plaintext));

        return JSON.stringify({
            v: ENVELOPE_VERSION,
            iv: base64urlEncode(iv),
            ct: base64urlEncode(new Uint8Array(ciphertext))
        });
    }

    async function sealText(keyBytes, board, plaintext) {
        return sealWithIV(keyBytes, board, plaintext, window.crypto.getRandomValues(new Uint8Array(IV_BYTES)));
    }

    async function openEnvelope(keyBytes, board, envelope) {
        var parsed;
        try {
            parsed = JSON.parse(envelope);
        } catch (error) {
            throw new Error('the board holds something that is not a sealed envelope');
        }
        if (!parsed || parsed.v !== ENVELOPE_VERSION) {
            throw new Error('this envelope was written by a version of GoSpin this page does not know');
        }

        var iv = base64urlDecode(parsed.iv);
        var ciphertext = base64urlDecode(parsed.ct);
        if (!iv || iv.length !== IV_BYTES || !ciphertext || ciphertext.length < 16) {
            throw new Error('the envelope is malformed');
        }

        var key = await importKey(keyBytes);
        try {
            var plaintext = await window.crypto.subtle.decrypt({
                name: 'AES-GCM',
                iv: iv,
                additionalData: new TextEncoder().encode(AAD_PREFIX + board),
                tagLength: TAG_BITS
            }, key, ciphertext);
            return new TextDecoder().decode(plaintext);
        } catch (error) {
            throw new Error('this key does not open the board: the link is truncated, the board belongs to another key, or the envelope was damaged');
        }
    }

    // boardFromPath returns the board name of /ui/sealed/<board>, or '' for the
    // create page.
    function boardFromPath() {
        var prefix = '/ui/sealed';
        var path = window.location.pathname;
        if (path.indexOf(prefix) !== 0) {
            return '';
        }
        var rest = path.slice(prefix.length).replace(/^\/+/, '').replace(/\/+$/, '');
        if (!rest) {
            return '';
        }
        try {
            return decodeURIComponent(rest);
        } catch (error) {
            return rest;
        }
    }

    function hashKey() {
        var fragment = window.location.hash.replace(/^#/, '');
        return fragment.indexOf('k=') === 0 ? fragment.slice(2) : fragment;
    }

    // request performs one API call. Sealed reads are never cached: a cached
    // envelope would let a burned board be read twice.
    async function request(path, options) {
        var init = Object.assign({ cache: 'no-store', credentials: 'same-origin' }, options || {});
        var response = await fetch(path, init);
        var text = await response.text();
        return { status: response.status, text: text, response: response };
    }

    function errorMessage(payload, fallback) {
        try {
            var parsed = JSON.parse(payload);
            if (parsed && parsed.error) {
                return parsed.error;
            }
        } catch (error) {
            // not JSON: the status is all there is
        }
        return fallback;
    }

    function boardPath(board) {
        return '/ghostboard/' + encodeURIComponent(board);
    }

    window.sealedBoard = function sealedBoard() {
        return {
            mode: 'create',
            board: '',
            secure: true,
            error: '',
            notice: '',
            maxPlaintext: 0,

            // create flow
            name: '',
            text: '',
            burn: true,
            link: '',
            creating: false,
            copied: false,

            // reveal flow
            keyText: '',
            keyBytes: null,
            locked: false,
            pin: '',
            pinError: '',
            unlocking: false,
            awaitingOpen: false,
            burned: false,
            plaintext: '',
            revealed: false,
            saving: false,
            saved: false,

            init: function () {
                this.secure = isSupported();
                this.maxPlaintext = parseInt(document.body.dataset.maxPlaintextBytes || '0', 10) || 0;
                this.board = boardFromPath();

                if (!this.board) {
                    this.mode = 'create';
                    return;
                }

                this.mode = 'reveal';
                this.keyText = hashKey();
                this.keyBytes = parseKey(this.keyText);
                if (!this.keyBytes) {
                    this.error = 'This link carries no usable key. Ask the sender for the whole link, including the part after #.';
                    return;
                }
                // A burn board cannot be read with a plain GET, so the page says
                // what it will do instead of fetching on load: a fetch would burn
                // the note before the reader clicked.
                this.awaitingOpen = true;
            },

            plaintextLimitLabel: function () {
                if (!this.maxPlaintext) {
                    return '';
                }
                return this.maxPlaintext + ' bytes of text';
            },

            nameValid: function () {
                return BOARD_PATTERN.test(this.name);
            },

            textBytes: function () {
                return new TextEncoder().encode(this.text).length;
            },

            create: async function () {
                this.error = '';
                this.notice = '';
                this.link = '';

                if (!this.secure) {
                    this.error = 'This browser refuses WebCrypto outside HTTPS. Open this page over HTTPS, or use gb-cli.';
                    return;
                }
                if (!this.nameValid()) {
                    this.error = 'Use 1-64 letters, digits, dots, dashes or underscores for the board name.';
                    return;
                }
                if (!this.text) {
                    this.error = 'Write something to seal first.';
                    return;
                }
                if (this.maxPlaintext && this.textBytes() > this.maxPlaintext) {
                    this.error = 'The text is too long for this server: ' + this.plaintextLimitLabel() + ' fit in one sealed board.';
                    return;
                }

                this.creating = true;
                try {
                    var key = newKey();
                    var envelope = await sealText(key, this.name, this.text);
                    var result = await request(boardPath(this.name), {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ text: envelope, mode: 'sealed', burn: this.burn })
                    });

                    if (result.status === 401) {
                        this.error = 'This board is private: open it from the ghostboard page once to unlock it, then come back.';
                        return;
                    }
                    if (result.status !== 200) {
                        this.error = errorMessage(result.text, 'The server refused the board (' + result.status + ').');
                        return;
                    }

                    this.link = window.location.origin + '/ui/sealed/' + encodeURIComponent(this.name) + '#k=' + base64urlEncode(key);
                    this.notice = this.burn
                        ? 'Sealed. This board destroys itself on the first open: hand the whole link over, fragment included.'
                        : 'Sealed. Every later write has to be sealed with the same key.';
                } catch (error) {
                    this.error = error.message || 'Sealing the board failed.';
                } finally {
                    this.creating = false;
                }
            },

            copyLink: async function () {
                if (!this.link) {
                    return;
                }
                try {
                    await navigator.clipboard.writeText(this.link);
                    this.copied = true;
                    setTimeout(function () { this.copied = false; }.bind(this), 2000);
                } catch (error) {
                    this.error = 'The clipboard is not available: select the link and copy it by hand.';
                }
            },

            // load reads a sealed board that is not a burn board. A burn board
            // answers 409 here and is opened with the explicit button instead.
            load: async function () {
                this.error = '';
                this.notice = '';

                if (!this.secure) {
                    this.error = 'This browser refuses WebCrypto outside HTTPS, so it cannot decrypt this board. Use gb-cli on the machine that holds the key.';
                    return;
                }
                if (!this.keyBytes) {
                    return;
                }

                try {
                    var result = await request(boardPath(this.board));
                    if (result.status === 401) {
                        this.locked = true;
                        return;
                    }
                    if (result.status === 410) {
                        this.burned = true;
                        this.error = 'This board was already burned and no longer exists.';
                        return;
                    }
                    if (result.status === 409) {
                        this.awaitingOpen = true;
                        return;
                    }
                    if (result.status !== 200) {
                        this.error = errorMessage(result.text, 'The server refused the board (' + result.status + ').');
                        return;
                    }

                    var state = JSON.parse(result.text);
                    if (!state || state.mode !== 'sealed') {
                        this.error = 'This board is not sealed: open it on the ghostboard page instead.';
                        return;
                    }
                    this.plaintext = await openEnvelope(this.keyBytes, this.board, state.text || '');
                    this.revealed = true;
                    this.awaitingOpen = false;
                } catch (error) {
                    this.error = error.message || 'Reading the board failed.';
                }
            },

            // open consumes a burn board. It is deliberately a click: a wrong key
            // or a truncated link still burns the note, which is the promise the
            // sender was given.
            open: async function () {
                this.error = '';
                this.notice = '';

                if (!this.secure) {
                    this.error = 'This browser refuses WebCrypto outside HTTPS, so it cannot decrypt this board. Use gb-cli on the machine that holds the key.';
                    return;
                }
                if (!this.keyBytes) {
                    return;
                }

                try {
                    var result = await request(boardPath(this.board) + '/open', { method: 'POST' });
                    if (result.status === 401) {
                        this.locked = true;
                        return;
                    }
                    if (result.status === 410) {
                        this.burned = true;
                        this.error = 'This board was already burned and no longer exists.';
                        return;
                    }
                    if (result.status === 409) {
                        // Not a burn board: read it with a plain GET instead.
                        this.awaitingOpen = false;
                        return this.load();
                    }
                    if (result.status !== 200) {
                        this.error = errorMessage(result.text, 'The server refused the board (' + result.status + ').');
                        return;
                    }

                    var state = JSON.parse(result.text);
                    this.burned = !!state.burn;
                    this.plaintext = await openEnvelope(this.keyBytes, this.board, state.text || '');
                    this.revealed = true;
                    this.awaitingOpen = false;
                    if (this.burned) {
                        this.notice = 'This board is gone now: it was destroyed the moment the server handed it over.';
                    }
                } catch (error) {
                    this.error = error.message || 'Opening the board failed.';
                }
            },

            save: async function () {
                this.error = '';
                this.notice = '';
                this.saving = true;
                try {
                    var envelope = await sealText(this.keyBytes, this.board, this.plaintext);
                    var result = await request(boardPath(this.board), {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ text: envelope, mode: 'sealed' })
                    });
                    if (result.status === 401) {
                        this.locked = true;
                        return;
                    }
                    if (result.status !== 200) {
                        this.error = errorMessage(result.text, 'The server refused the board (' + result.status + ').');
                        return;
                    }
                    this.saved = true;
                    setTimeout(function () { this.saved = false; }.bind(this), 2000);
                } catch (error) {
                    this.error = error.message || 'Sealing the board failed.';
                } finally {
                    this.saving = false;
                }
            },

            unlock: async function () {
                this.pinError = '';
                this.unlocking = true;
                try {
                    var result = await request(boardPath(this.board) + '/unlock', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ pin: this.pin })
                    });
                    if (result.status !== 200) {
                        this.pinError = result.status === 429 ? 'Too many attempts: wait a little and try again.' : 'That PIN was rejected.';
                        return;
                    }
                    this.locked = false;
                    this.pin = '';
                    if (this.awaitingOpen) {
                        return this.open();
                    }
                    return this.load();
                } catch (error) {
                    this.pinError = 'The server could not be reached.';
                } finally {
                    this.unlocking = false;
                }
            },

            copyText: async function () {
                try {
                    await navigator.clipboard.writeText(this.plaintext);
                    this.notice = 'Copied to the clipboard.';
                } catch (error) {
                    this.error = 'The clipboard is not available.';
                }
            }
        };
    };

    // sealedSelfTest seals the published vector with its published IV and
    // compares the result, so the browser implementation and the Go one cannot
    // drift apart silently. The end-to-end pass calls it from Playwright.
    window.sealedSelfTest = async function () {
        var response = await fetch(VECTOR_PATH, { cache: 'no-store' });
        if (!response.ok) {
            throw new Error('the known-answer vector is not served: ' + response.status);
        }
        var vector = await response.json();
        var key = parseKey(vector.key);
        var iv = base64urlDecode(vector.iv);
        if (!key || !iv) {
            throw new Error('the known-answer vector carries an unusable key or iv');
        }

        var envelope = await sealWithIV(key, vector.board, vector.plaintext, iv);
        if (envelope !== vector.envelope) {
            throw new Error('the sealed envelope differs from the published vector');
        }
        var plaintext = await openEnvelope(key, vector.board, vector.envelope);
        if (plaintext !== vector.plaintext) {
            throw new Error('the published envelope does not decrypt to its plaintext');
        }
        return true;
    };
})();
