# Piano di Reimplementazione UI con HTMX

**Data**: Febbraio 2026
**Obiettivo**: Riscrivere la UI completa con HTMX per migliorare manutenibilità, comprensibilità e semplicità di aggiornamento.
**Scope**: Mantenere la parità funzionale con l'UI attuale, eliminando la complessità JS.
**Prompt**: Analizza il codice della UI e dimmi se è complesso, quanto costerebbe, se ci sono vantaggi a reimplementare la UI usando htmx. Voglio riscrivere tutta la UI con htmlx, voglio farlo a scopo didattico e perchè voglio un codice che sia più facilmente comprensibile e quindi modificabile in futuro, inoltre voglio che l'aggiunta di nuove funzionalità lato gui siano più semplici e veloci con meno rischi di introduzione di bug e regressioni.
---

## 1. Problema e Approccio

### Problema Attuale
- UI monolitica in 1.600 LOC (664 HTML + 920 JS)
- Logica distribuita tra Alpine.js store e query selectors nel DOM
- Difficile aggiungere nuove feature GUI senza rischi di regressioni
- State management decentralizzato in Alpine.js
- File singoli difficili da navigare e modificare

### Obiettivo di Reimplementazione
- **Server-driven UI**: Logica di rendering nel backend Go
- **Template modulari**: Partials riusabili per ogni componente
- **JS minimale**: Solo validazione e progressive enhancement
- **Manutenibilità**: Codice più facile da comprendere e modificare
- **Scopo didattico**: Imparare pattern HTMX e templating lato server

### Benefici Attesi
✅ **JS ridotto**: 920 LOC → 150-200 LOC (80% riduzione)
✅ **Logica centralizzata**: Tutto il rendering nel backend
✅ **Riutilizzabilità**: Template partials per ridurre duplicazione
✅ **Meno bug**: Niente sync state issues tra client e server
✅ **Più facilità di testing**: HTML/handlers testabili separatamente

---

## 2. Architettura Proposta

### 2.1 Struttura Directory

```
gospin/
├── cmd/server/
│   ├── main.go
│   └── handlers/
│       ├── containers.go      (New: handlers per tabs)
│       ├── groups.go
│       ├── schedules.go
│       ├── modal_handlers.go
│       └── components.go       (Utility handlers per partials)
│
├── internal/
│   └── ui/                     (New: UI-specific logic)
│       ├── templates/          (Go html/template files)
│       │   ├── layout.html     (Base layout)
│       │   ├── tabs.html       (Tab navigation)
│       │   ├── containers/
│       │   │   ├── list.html
│       │   │   ├── row.html    (Single row, riusabile)
│       │   │   ├── modal.html
│       │   │   └── form.html
│       │   ├── groups/
│       │   │   ├── list.html
│       │   │   ├── item.html
│       │   │   └── modal.html
│       │   ├── schedules/
│       │   │   ├── list.html
│       │   │   ├── item.html
│       │   │   ├── form.html
│       │   │   └── modal.html
│       │   ├── components/     (Shared components)
│       │   │   ├── modal.html
│       │   │   ├── button.html
│       │   │   ├── form-group.html
│       │   │   └── confirm.html
│       │   └── errors/
│       │       ├── toast.html
│       │       └── validation.html
│       │
│       └── renderer.go         (Utility per renderizzare templates)
│
└── ui/
    ├── index.html              (Scaffolding base, minimal)
    ├── assets/
    │   ├── style.css           (Tailwind + custom)
    │   └── main.js             (100 LOC: validation, PWA, utilities)
    └── pwa/                    (Mantenere come è)
```

### 2.2 Flusso di Interazione

**Pattern HTMX standard:**
```
User Action (click, submit)
    ↓
HTMX intercepts → XHR Request
    ↓
Go Handler processes → Renders template fragment
    ↓
HTML response ←
    ↓
HTMX swaps DOM element
    ↓
Browser renders (CSS Tailwind applies automatically)
```

**Esempio concreto:**
```
Utente clicca "Start container"
  ↓
<button hx-post="/api/containers/123/start"
        hx-target="#container-123"
        hx-swap="outerHTML">
  ↓
Handler POST /api/containers/123/start
  ↓
Go: container.Start() → Re-render row con status=running
  ↓
return Render(w, "containers/row.html", data)
  ↓
HTMX riceve: <tr id="container-123" ...>Running...</tr>
  ↓
DOM swap avviene, UI aggiornata
```

### 2.3 Handlers Go da Creare

| Handler | Endpoint | Responsabilità |
|---------|----------|-----------------|
| `GetDashboard` | GET / | Render layout + tabs |
| `GetContainersList` | GET /containers | List + table |
| `PostContainerAction` | POST /containers/:id/:action | Start/stop/restart |
| `GetContainerModal` | GET /modals/container/:id | Edit form |
| `PostContainerCreate` | POST /containers | Create container |
| `GetGroupsList` | GET /groups | List + items |
| `GetSchedulesList` | GET /schedules | List + items |
| `PostScheduleSubmit` | POST /schedules | Create/update |
| `PostModalConfirm` | POST /modals/confirm/:action | Delete confirmation |
| `GetStats` | GET /api/stats/:id | OOB update stats |

---

## 3. Workplan Dettagliato

### Fase 1: Setup e Preparazione (2 giorni)

- [ ] **1.1** Creare struttura directory `internal/ui/templates`
- [ ] **1.2** Impostare package Go `ui` con funzione `Render(w, template, data)`
- [ ] **1.3** Aggiungere handler di base: `GetDashboard` (scaffolding)
- [ ] **1.4** Verificare che template Go compila senza errori
- [ ] **1.5** Impostare hot-reload per template (aggiornare Air config)
- [ ] **1.6** Cleanup: Rimuovere dipendenze inutili da `main.js` (Alpine.js store)

### Fase 2: Layout Base e Navigazione (3 giorni)

- [ ] **2.1** Creare `layout.html` (base structure, navbar, tabs)
- [ ] **2.2** Creare `tabs.html` (tab navigation + HTMX swap)
- [ ] **2.3** Implementare handler `GetDashboard` che renderizza layout
- [ ] **2.4** Implementare handler per tab swap:
  - [ ] `GET /tab/containers` → renderizza lista container
  - [ ] `GET /tab/groups` → renderizza lista groups
  - [ ] `GET /tab/schedules` → renderizza lista schedules
- [ ] **2.5** Testare navigazione tra tab (HTMX swap funzionante)
- [ ] **2.6** Verificare CSS Tailwind applica correttamente

### Fase 3: Componente Containers (4 giorni)

#### 3.1 Rendering Lista

- [ ] **3.1.1** Creare `containers/list.html` (table structure)
- [ ] **3.1.2** Creare `containers/row.html` (single row, parametrizzato)
- [ ] **3.1.3** Implementare handler `GetContainersList`
- [ ] **3.1.4** Aggiungere sorting HTMX (header clicca → swap list ordinata)
- [ ] **3.1.5** Aggiungere filtering HTMX (input filter → swap list filtrata)

#### 3.2 Azioni Container

- [ ] **3.2.1** Creare button "Start/Stop/Restart" con `hx-post`
- [ ] **3.2.2** Implementare handler `PostContainerAction`
- [ ] **3.2.3** Handler aggiorna stato container → render row aggiornata
- [ ] **3.2.4** Testare feedback visivo immediato (status change color)

#### 3.3 Stats Real-time

- [ ] **3.3.1** Creare `containers/stats-cell.html` (CPU/Memory display)
- [ ] **3.3.2** Aggiungere `hx-trigger="every 60s"` per auto-refresh
- [ ] **3.3.3** Implementare handler `GetStats/:id` per OOB update
- [ ] **3.3.4** Testare polling non bloccante

#### 3.4 Modal Container

- [ ] **3.4.1** Creare `containers/modal.html` (create/edit form)
- [ ] **3.4.2** Implementare handler `GetContainerModal/:id` (pre-populate)
- [ ] **3.4.3** Creare `containers/form.html` (form fields riusabile)
- [ ] **3.4.4** Implementare handler `PostContainerCreate` con validazione
- [ ] **3.4.5** Testare form submission e errore handling

### Fase 4: Componente Groups (2 giorni)

- [ ] **4.1** Creare `groups/list.html` (item list)
- [ ] **4.2** Creare `groups/item.html` (single item, riusabile)
- [ ] **4.3** Implementare handler `GetGroupsList`
- [ ] **4.4** Creare `groups/modal.html` (create/edit con container selector)
- [ ] **4.5** Implementare handler `PostGroupCreate`
- [ ] **4.6** Implementare handler `PostGroupDelete` con confirm
- [ ] **4.7** Testare multi-select checkboxes funzionano

### Fase 5: Componente Schedules (3 giorni)

- [ ] **5.1** Creare `schedules/list.html` (item list)
- [ ] **5.2** Creare `schedules/item.html` (single item)
- [ ] **5.3** Creare `schedules/form.html` (day/time picker)
- [ ] **5.4** Creare `schedules/modal.html` (form + target selector)
- [ ] **5.5** Implementare handler `GetSchedulesList`
- [ ] **5.6** Implementare handler `PostScheduleCreate` con validazione
- [ ] **5.7** Implementare handler `PostScheduleUpdate`
- [ ] **5.8** Implementare handler `PostScheduleDelete` con confirm
- [ ] **5.9** Testare logic timer picker (day+time validation)

### Fase 6: Componenti Condivisi e Modali (2 giorni)

- [ ] **6.1** Creare `components/modal-wrapper.html` (riusabile)
- [ ] **6.2** Creare `components/button.html` (standardizzato)
- [ ] **6.3** Creare `components/form-group.html` (input group riusabile)
- [ ] **6.4** Creare `components/confirm-dialog.html` (delete confirmation)
- [ ] **6.5** Creare `errors/toast.html` (success/error messages)
- [ ] **6.6** Creare `errors/validation.html` (form validation errors)
- [ ] **6.7** Implementare handler `PostModalConfirm/:action`

### Fase 7: Riduzione JavaScript (1 giorno)

- [ ] **7.1** Identificare JS che NON servono più (Alpine store è rimosso)
- [ ] **7.2** Mantenere SOLO:
  - [ ] Validazione client-side (regex patterns, required fields)
  - [ ] PWA service worker init
  - [ ] Touch/swipe detection (se necessario)
  - [ ] Utility CSS-in-JS (se necessario)
- [ ] **7.3** Ridurre `main.js` da 920 → ~150 LOC
- [ ] **7.4** Aggiungere commenti documentazione nel nuovo `main.js`

### Fase 8: Styling e Responsive Design (2 giorni)

- [ ] **8.1** Verificare Tailwind classes applicate correttamente
- [ ] **8.2** Testare responsive su mobile (breakpoint sm, md, lg)
- [ ] **8.3** Verificare colori/tema consistenti
- [ ] **8.4** Testare dark mode (se applicabile)
- [ ] **8.5** Aggiustamenti CSS custom necessari
- [ ] **8.6** Verificare accessibilità (ARIA labels, tab order)

### Fase 9: Testing e QA (3 giorni)

#### 9.1 Unit Testing

- [ ] **9.1.1** Testare ogni handler Go con `testing.T`
- [ ] **9.1.2** Mock API responses per template testing
- [ ] **9.1.3** Testare validazione form lato server
- [ ] **9.1.4** Coverage minimo 70% per package `ui`

#### 9.2 Integration Testing

- [ ] **9.2.1** Testare flow container start/stop/restart
- [ ] **9.2.2** Testare flow group create/edit/delete
- [ ] **9.2.3** Testare flow schedule create/update/delete
- [ ] **9.2.4** Testare error scenarios (validazione fallita, API error)
- [ ] **9.2.5** Testare sorting/filtering da lista

#### 9.3 Functional Testing (Manual)

- [ ] **9.3.1** Testare su Chrome/Firefox/Safari
- [ ] **9.3.2** Testare su mobile device (iOS/Android)
- [ ] **9.3.3** Verificare HTMX swaps visualmente corretti
- [ ] **9.3.4** Verificare stats refresh ogni 60s
- [ ] **9.3.5** Testare offline mode (PWA service worker)
- [ ] **9.3.6** Verificare performance (network tab, rendering time)

#### 9.4 Regressione

- [ ] **9.4.1** Testare tutti endpoint API GET/POST
- [ ] **9.4.2** Verificare no data loss da vecchia UI
- [ ] **9.4.3** Verificare stato persistente tra reload pagina

### Fase 10: Cleanup e Documentazione (1 giorno)

- [ ] **10.1** Rimuovere file UI vecchia (`ui/index.html` se completamente rimpiazzato)
- [ ] **10.2** Aggiornare `README.md` con nuova architettura UI
- [ ] **10.3** Aggiornare `docs/ARCHITECTURE.md` con sezione UI/Template
- [ ] **10.4** Documentare pattern HTMX usati (GET/POST/swap)
- [ ] **10.5** Creare guida per aggiungere nuove feature UI
- [ ] **10.6** Aggiornare `go.mod` se necessario (templ, htmx versioning)
- [ ] **10.7** Verificare Makefile build/run funziona correttamente
- [ ] **10.8** Test finale end-to-end: `make run` → UI carica → Tutte le feature funzionano

---

## 4. Considerazioni Tecniche

### 4.1 Template Go (`html/template`)

**Pro:**
- Integrato in stdlib Go
- No dependencies external
- Type-safe parameter passing
- Automatic HTML escaping (security)

**Pattern da usare:**
```go
// Template con nested partials
{{ template "components/button.html" .Button }}

// Loops e conditionals
{{ range .Containers }}
  {{ template "containers/row.html" . }}
{{ end }}

// Template functions custom
{{ getContainerStatus .ID | print }}
```

### 4.2 HTMX Attributes Pattern

**Uso coerente:**
- `hx-get` per fetching data (GET-safe)
- `hx-post` per azioni modificative
- `hx-target` specifica elemento da sostituire
- `hx-swap` specifica modalità swap (innerhtml, outerhtml, beforeend)
- `hx-trigger` per custom events
- `hx-confirm` per confirmation dialogs

**Esempio:**
```html
<!-- Fetch e replace -->
<button hx-get="/containers/123/details"
        hx-target="#details-panel"
        hx-swap="innerHTML">
  View Details
</button>

<!-- Post con confirmation -->
<button hx-post="/containers/123/delete"
        hx-confirm="Delete this container?"
        hx-target="closest tr"
        hx-swap="outerHTML swap:1s">
  Delete
</button>

<!-- Auto-refresh stats -->
<div hx-get="/api/stats/123"
     hx-target="this"
     hx-swap="innerHTML"
     hx-trigger="every 60s">
  CPU: 45%
</div>
```

### 4.3 Error Handling

**Lato server:**
```go
// Se errore, ritornare HTTP status + error toast template
if err != nil {
  w.WriteHeader(http.StatusBadRequest)
  return renderTemplate(w, "errors/toast.html", ErrorData{
    Type: "error",
    Message: err.Error(),
  })
}
```

**HTMX intercetterà 4xx e NON farà swap, mostrerà errore.**

### 4.4 State Management: Niente Più Alpine Store

**Dato che tutto rendering nel backend:**
- Server mantiene stato container/groups/schedules in memoria o DB
- Client NON mantiene state locale
- Browser è "stateless view layer"
- Ricarica pagina = fetch dati fresh dal server

**Vantaggio:** No sync issues, source of truth unica.

### 4.5 Performance

**Misure di mitigazione per latency HTTP:**

1. **Caching Header:** Aggiungere Cache-Control per static assets
2. **Compression:** GZIP response HTML/CSS
3. **Lazy loading:** Stats fetch solo se visibile
4. **Request batching:** Un endpoint multiplo può ritornare più partials

**Expected latency:**
- Tab switch: ~50-100ms (rendering Go + network)
- Action (start/stop): ~100-200ms (API call + render)
- Stats refresh: ~50ms (OOB update)

---

## 5. Metriche di Successo

| Metrica | Target | Note |
|---------|--------|------|
| **JS LOC** | < 200 | Da 920 |
| **Template files** | 12-15 | Modularità |
| **Handler functions** | 15-20 | One concern each |
| **Code coverage** | ≥ 70% | Per UI package |
| **Load time** | < 500ms | Per prima richiesta |
| **Tab switch latency** | < 200ms | UX feel |
| **Test coverage** | 100% handler paths | Critical flows |
| **Documentation** | Completo | Guida per nuove feature |

---

## 6. Timeline Estimata

| Fase | Giorni | % Completo |
|------|--------|-----------|
| 1. Setup | 2 | 5% |
| 2. Layout | 3 | 15% |
| 3. Containers | 4 | 40% |
| 4. Groups | 2 | 55% |
| 5. Schedules | 3 | 75% |
| 6. Components | 2 | 85% |
| 7. JS cleanup | 1 | 87% |
| 8. Styling | 2 | 92% |
| 9. Testing | 3 | 98% |
| 10. Docs | 1 | 100% |
| **TOTALE** | **23 giorni** | |

**Per developer scopo didattico:** ~4-5 settimane part-time (4h/giorno).

---

## 7. Rischi e Mitigation

| Rischio | Probabilità | Impatto | Mitigation |
|---------|-------------|--------|-----------|
| HTMX swap fallisce | Bassa | Alto | Test su 3 browser prima release |
| Template Go compila lento | Bassa | Medio | Cache template, check build time |
| Performance degrada | Bassa | Medio | Monitor stats latency con DevTools |
| Regression features | Media | Alto | Test suite coprire 100% user flows |
| PWA offline mode break | Bassa | Medio | Testare service worker con offline |

---

## 8. Rollback Plan

Se decisione reverting (unlikely ma possibile):

1. **Versioning:** Keep old UI in branch `ui/alpine-backup`
2. **Compatibility:** Nuovi endpoint Go supportano BOTH template + JSON (dual-render)
3. **Toggle:** Feature flag per switch template vs JSON
4. **Timeline:** Rollback < 1 ora se deciso nel primo mese

---

## 9. Note Finali

✅ **Questo progetto è ideale per:**
- Learning HTMX patterns in production context
- Refactoring da SPA leggera a SSR leggera
- Migliorare code maintainability senza riscrivere backend

✅ **Vostri vantaggi specifici:**
- Stack minimalista (no Node.js, no npm) rimane
- PWA capability si mantiene (service worker stays)
- Go backend rimane punto di forza
- Niente dipendenze critiche nuove

✅ **Success criteria:**
- UI compresa da nuovo developer in < 2 ore
- Aggiunta nuova feature (es: container logs) < 1 ora
- Niente regression da UI attuale
- Codice didattico per futuri maintainers

---

**Piano approvato e pronto per implementazione! 🚀**

Contattare per chiarimenti su qualsiasi fase.
