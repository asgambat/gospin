// GoSpin UI - Alpine.js Application
function app() {
    return {
            // Auto-refresh interval (seconds)
            refreshInterval: 60, // default 60 seconds
            refreshTimer: null,
            // Stats refresh interval (seconds)
            statsRefreshInterval: 120, // default 120 seconds
            statsRefreshTimer: null,
            // Container stats map (name -> {cpu, mem})
            containerStats: {},
            // Show CPU/MEM columns (responsive)
            showStatsColumns: true,
            // Show Friendly Name and Active columns (responsive for very small screens)
            showMetaColumns: true,
            // Show Containers/Active columns in Groups tab (responsive <500px)
            showGroupMetaColumns: true,
            // Stack action buttons under small screens (<400px)
            showStackButtons: false,
            // Container refresh loading state
            isContainerRefreshing: false,
        // State
        activeTab: 'containers',
        containers: [],
        groups: [],
        schedules: [],
        error: '',
        success: '',
        
        // Modals
        showContainerModal: false,
        showGroupModal: false,
        showScheduleModal: false,
        // Confirmation modal
        showConfirmModal: false,
        confirmTitle: '',
        confirmMessage: '',
        _confirmResolve: null,
        // Details modal for a container (read-only)
        showContainerDetailsModal: false,
        detailsContainer: {
            name: '',
            friendly_name: '',
            url: '',
            running: false,
            active: false
        },
        // Details modal for a group (read-only)
        showGroupDetailsModal: false,
        detailsGroup: {
            name: '',
            container: [],
            active: false
        },
        
        // Runtime containers for autocomplete
        runtimeContainers: [],
        filteredRuntimeContainers: [],
        showContainerSuggestions: false,
        
        // Editing flags
        editingContainer: false,
        editingGroup: false,
        editingSchedule: false,
        
        // Forms
        containerForm: {
            name: '',
            friendly_name: '',
            url: '',
            running: false,
            active: true
        },
        groupForm: {
            name: '',
            container: [],
            active: true
        },
        scheduleForm: {
            id: '',
            target: '',
            targetType: 'container',
            timers: []
        },
        
        // Day names for display
        dayNames: ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'],
        
        // API base URL (same origin)
        apiBase: '',
        
        // Sorting and filtering for containers
        containerSort: { key: 'name', asc: true },
        containerFilter: { name: '' },
        
        // Sorting and filtering for groups
        groupSort: { key: 'name', asc: true },
        groupFilter: { name: '' },
        
        // Server configuration
        configuration: {
            baseUrl: '',
            spinUpUrl: '',
            statsRefreshIntervalSec: 120
        },
        
        // Initialize
        async init() {
            // initialize responsive state and listen to resize
            const updateResponsive = () => {
                const w = window.innerWidth;
                this.showStatsColumns = w > 1200;
                this.showMetaColumns = w >= 600;
                this.showGroupMetaColumns = w >= 500;
                this.showStackButtons = w < 400;
            };
            updateResponsive();
            window.addEventListener('resize', updateResponsive);

            // Setup swipe/mouse-drag for delete
            this.setupSwipeToDelete();

            await this.loadAll();
            await this.loadContainerStats();
            this.startAutoRefresh();
            this.startStatsRefresh();
        },

        // Setup swipe and mouse-drag to delete
        setupSwipeToDelete() {
            const self = this;
            // Helper to get the closest swipe-track element
            function getSwipeTrackEl(target) {
                while (target && !target.classList?.contains('swipe-track')) {
                    target = target.parentElement;
                }
                return target;
            }
            // Touch events
            let startX = 0, currentX = 0, dragging = false, swipeEl = null;
            let mouseDown = false;
            const threshold = 60; // px
            function onStart(e) {
                const touch = e.touches ? e.touches[0] : e;
                swipeEl = getSwipeTrackEl(e.target);
                if (!swipeEl) return;
                startX = touch.clientX;
                currentX = startX;
                dragging = true;
                swipeEl.classList.remove('swipe-delete-anim', 'swipe-delete-anim-left', 'swipe-delete');
            }
            function onMove(e) {
                if (!dragging || !swipeEl) return;
                const touch = e.touches ? e.touches[0] : e;
                currentX = touch.clientX;
                const dx = currentX - startX;
                if (Math.abs(dx) > 10) {
                    swipeEl.classList.add('swipe-delete');
                    if (dx > 0) {
                        swipeEl.classList.add('swipe-delete-anim');
                        swipeEl.classList.remove('swipe-delete-anim-left');
                    } else {
                        swipeEl.classList.add('swipe-delete-anim-left');
                        swipeEl.classList.remove('swipe-delete-anim');
                    }
                }
            }
            function onEnd() {
                if (!dragging || !swipeEl) return;
                const dx = currentX - startX;
                if (Math.abs(dx) > threshold) {
                    // Execute delete
                    const name = swipeEl.getAttribute('data-name');
                    const type = swipeEl.getAttribute('data-type');
                    if (type === 'container') {
                        self.deleteContainer(name);
                    } else if (type === 'group') {
                        self.deleteGroup(name);
                    } else if (type === 'schedule') {
                        self.deleteSchedule(name);
                    }
                }
                // Reset feedback
                if (swipeEl) {
                    swipeEl.classList.remove('swipe-delete', 'swipe-delete-anim', 'swipe-delete-anim-left');
                }
                dragging = false;
                swipeEl = null;
            }
            // Touch events
            document.addEventListener('touchstart', onStart, { passive: true });
            document.addEventListener('touchmove', onMove, { passive: true });
            document.addEventListener('touchend', onEnd, { passive: true });
            // Mouse events
            document.addEventListener('mousedown', function(e) {
                if (e.button !== 0) return;
                mouseDown = true;
                onStart(e);
            });
            document.addEventListener('mousemove', function(e) {
                if (!mouseDown) return;
                onMove(e);
            });
            document.addEventListener('mouseup', function() {
                if (!mouseDown) return;
                mouseDown = false;
                onEnd();
            });
        },
        
        async loadAll() {
            await Promise.all([
                this.loadContainers(),
                this.loadGroups(),
                this.loadSchedules(),
                this.loadConfiguration()
            ]);
        },

        // Start auto-refresh timer
        startAutoRefresh() {
            if (this.refreshTimer) {
                clearInterval(this.refreshTimer);
            }
            this.refreshTimer = setInterval(() => {
                this.loadAll();
            }, this.refreshInterval * 1000);
        },

        // Allow changing refresh interval at runtime
        setRefreshInterval(seconds) {
            // Only restart timer if interval actually changed
            if (this.refreshInterval === seconds && this.refreshTimer) {
                return;
            }
            this.refreshInterval = seconds;
            this.startAutoRefresh();
        },

        // Start stats refresh timer
        startStatsRefresh() {
            if (this.statsRefreshTimer) {
                clearInterval(this.statsRefreshTimer);
            }
            this.statsRefreshTimer = setInterval(() => {
                this.loadContainerStats();
            }, this.statsRefreshInterval * 1000);
        },

        // Allow changing stats refresh interval at runtime
        setStatsRefreshInterval(seconds) {
            // Only restart timer if interval actually changed
            if (this.statsRefreshInterval === seconds && this.statsRefreshTimer) {
                return;
            }
            this.statsRefreshInterval = seconds;
            this.startStatsRefresh();
        },

        // Load container stats from runtime/stats endpoint
        async loadContainerStats() {
            try {
                const res = await fetch(`${this.apiBase}/runtime/stats`);
                if (!res.ok) throw new Error(await res.text());
                const stats = await res.json();
                // Build a map by container name
                const statsMap = {};
                for (const s of stats) {
                    if (s.error) {
                        statsMap[s.name] = { cpu: 0, mem: 0 };
                    } else {
                        statsMap[s.name] = { cpu: s.cpu_percent, mem: s.memory_mb };
                    }
                }
                this.containerStats = statsMap;
            } catch (e) {
                // On error, don't show error to user, just reset stats
                this.containerStats = {};
            }
        },

        // Refresh containers and stats with loading state and client-side timeout (5 minutes)
        async refreshContainersWithStats() {
            if (this.isContainerRefreshing) return;
            this.isContainerRefreshing = true;
            const CLIENT_TIMEOUT_MS = 5 * 60 * 1000; // 5 minutes
            const timeoutPromise = new Promise((_, reject) => 
                setTimeout(() => reject(new Error('Client timeout: refresh took too long')), CLIENT_TIMEOUT_MS)
            );
            try {
                await Promise.race([
                    Promise.all([this.loadContainers(), this.loadContainerStats()]),
                    timeoutPromise
                ]);
            } catch (e) {
                this.showError('Failed to refresh: ' + e.message);
            } finally {
                this.isContainerRefreshing = false;
            }
        },

        // Get CPU stat for a container
        getContainerCpu(name) {
            return this.containerStats[name]?.cpu ?? 0;
        },

        // Get Memory stat for a container
        getContainerMem(name) {
            return this.containerStats[name]?.mem ?? 0;
        },
        
        async loadConfiguration() {
            try {
                const res = await fetch(`${this.apiBase}/configuration`);
                if (!res.ok) throw new Error(await res.text());
                this.configuration = await res.json();
                // Se il server fornisce refreshIntervalSec, aggiorna l'intervallo di refresh
                if (this.configuration.refreshIntervalSec && Number.isFinite(this.configuration.refreshIntervalSec)) {
                    this.setRefreshInterval(this.configuration.refreshIntervalSec);
                }
                // Se il server fornisce statsRefreshIntervalSec, aggiorna l'intervallo di refresh stats
                if (this.configuration.statsRefreshIntervalSec && Number.isFinite(this.configuration.statsRefreshIntervalSec)) {
                    this.setStatsRefreshInterval(this.configuration.statsRefreshIntervalSec);
                }
            } catch (e) {
                this.showError('Failed to load configuration: ' + e.message);
            }
        },
        
        // Computed: filtered and sorted containers
        get filteredSortedContainers() {
            let arr = [...this.containers];
            // Filtering
            if (this.containerFilter.name.trim() !== '') {
                arr = arr.filter(c => c.name.toLowerCase().includes(this.containerFilter.name.trim().toLowerCase()));
            }
            // Sorting
            const { key, asc } = this.containerSort;
            arr.sort((a, b) => {
                let va, vb;
                if (key === 'cpu') {
                    va = this.containerStats[a.name]?.cpu ?? 0;
                    vb = this.containerStats[b.name]?.cpu ?? 0;
                } else if (key === 'mem') {
                    va = this.containerStats[a.name]?.mem ?? 0;
                    vb = this.containerStats[b.name]?.mem ?? 0;
                } else if (key === 'active' || key === 'running') {
                    va = !!a[key] ? 1 : 0;
                    vb = !!b[key] ? 1 : 0;
                } else {
                    va = a[key] ? a[key].toString().toLowerCase() : '';
                    vb = b[key] ? b[key].toString().toLowerCase() : '';
                }
                if (va < vb) return asc ? -1 : 1;
                if (va > vb) return asc ? 1 : -1;
                return 0;
            });
            return arr;
        },
        
        // Change sorting for containers
        sortContainersBy(key) {
            if (this.containerSort.key === key) {
                this.containerSort.asc = !this.containerSort.asc;
            } else {
                this.containerSort.key = key;
                this.containerSort.asc = true;
            }
        },
        
        // Computed: filtered and sorted groups
        get filteredSortedGroups() {
            let arr = [...this.groups];
            // Filtering
            if (this.groupFilter.name.trim() !== '') {
                arr = arr.filter(g => g.name.toLowerCase().includes(this.groupFilter.name.trim().toLowerCase()));
            }
            // Sorting
            const { key, asc } = this.groupSort;
            arr.sort((a, b) => {
                let va = a[key], vb = b[key];
                if (key === 'active') {
                    va = !!va ? 1 : 0;
                    vb = !!vb ? 1 : 0;
                } else {
                    va = va ? va.toString().toLowerCase() : '';
                    vb = vb ? vb.toString().toLowerCase() : '';
                }
                if (va < vb) return asc ? -1 : 1;
                if (va > vb) return asc ? 1 : -1;
                return 0;
            });
            return arr;
        },
        
        // Change sorting for groups
        sortGroupsBy(key) {
            if (this.groupSort.key === key) {
                this.groupSort.asc = !this.groupSort.asc;
            } else {
                this.groupSort.key = key;
                this.groupSort.asc = true;
            }
        },
        
        // Generate URL based on container name and baseUrl configuration
        generateContainerUrl(name) {
            const baseUrl = this.configuration.baseUrl || '';
            
            if (!baseUrl || baseUrl.trim() === '') {
                // If baseUrl is empty, use localhost
                return `http://localhost/${name}`;
            }
            
            if (baseUrl.includes('$1')) {
                // If baseUrl contains $1 token, replace it with the container name
                return baseUrl.replace('$1', name);
            }
            
            // Otherwise, append the name to baseUrl, avoiding double slashes
            let url = baseUrl;
            if (!url.endsWith('/')) {
                url += '/';
            }
            url += name;
            // Remove any double slashes (except after protocol)
            return url.replace(/([^:])(\/\/+)/g, '$1/');
        },
        
        // Generate SpinUp URL based on friendly_name and spinUpUrl configuration
        generateSpinUpUrl(friendlyName) {
            const spinUpUrl = this.configuration.spinUpUrl || '';
            
            if (!spinUpUrl || spinUpUrl.trim() === '') {
                // If spinUpUrl is empty, return empty string
                return '';
            }
            
            if (spinUpUrl.includes('$1')) {
                // If spinUpUrl contains $1 token, replace it with the friendly_name
                return spinUpUrl.replace('$1', friendlyName);
            }
            
            // Otherwise, append the friendly_name to spinUpUrl, avoiding double slashes
            let url = spinUpUrl;
            if (!url.endsWith('/')) {
                url += '/';
            }
            url += friendlyName;
            // Remove any double slashes (except after protocol)
            return url.replace(/([^:])(\/\/+)/g, '$1/');
        },
        
        // ==================== CONTAINERS ====================
        async loadContainers() {
            try {
                const res = await fetch(`${this.apiBase}/containers`);
                if (!res.ok) throw new Error(await res.text());
                this.containers = await res.json();
            } catch (e) {
                this.showError('Failed to load containers: ' + e.message);
            }
        },
        
        async loadRuntimeContainers() {
            try {
                const res = await fetch(`${this.apiBase}/runtime/containers`);
                if (!res.ok) throw new Error(await res.text());
                this.runtimeContainers = await res.json();
                this.filteredRuntimeContainers = [...this.runtimeContainers];
            } catch (e) {
                this.showError('Failed to load runtime containers: ' + e.message);
                this.runtimeContainers = [];
                this.filteredRuntimeContainers = [];
            }
        },
        
        filterContainerSuggestions() {
            const search = this.containerForm.name.toLowerCase();
            if (search === '') {
                this.filteredRuntimeContainers = [...this.runtimeContainers];
            } else {
                this.filteredRuntimeContainers = this.runtimeContainers.filter(name => 
                    name.toLowerCase().includes(search)
                );
            }
        },
        
        selectContainerName(name) {
            this.containerForm.name = name;
            // Auto-populate friendly_name with the same value
            this.containerForm.friendly_name = name;
            // Auto-populate URL based on configuration
            this.containerForm.url = this.generateContainerUrl(name);
            // Delay la chiusura per evitare conflitti con Alpine @click.away
            setTimeout(() => { this.showContainerSuggestions = false; }, 100);
        },
        
        async openContainerModal(container = null) {
            if (container) {
                this.editingContainer = true;
                this.containerForm = {
                    name: container.name,
                    friendly_name: container.friendly_name,
                    url: container.url,
                    running: container.running || false,
                    active: container.active || false
                };
                this.showContainerSuggestions = false;
            } else {
                this.editingContainer = false;
                this.containerForm = {
                    name: '',
                    friendly_name: '',
                    url: '',
                    running: false,
                    active: true
                };
                await this.loadRuntimeContainers();
                this.showContainerSuggestions = false;
            }
            this.showContainerModal = true;
        },
        
        async saveContainer() {
            try {
                const payload = {
                    name: this.containerForm.name,
                    friendly_name: this.containerForm.friendly_name,
                    url: this.containerForm.url,
                    running: this.containerForm.running,
                    active: this.containerForm.active
                };
                const res = await fetch(`${this.apiBase}/container`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                });
                if (!res.ok) {
                    const err = await res.json();
                    throw new Error(err.error || 'Save failed');
                }
                // Dopo il salvataggio, ricarica i container per aggiornare lo stato running
                await this.loadContainers();
                this.showContainerModal = false;
                this.showSuccess('Container saved successfully');
            } catch (e) {
                this.showError('Failed to save container: ' + e.message);
            }
        },

        openContainerDetails(container) {
            this.detailsContainer = {
                name: container.name,
                friendly_name: container.friendly_name,
                url: container.url,
                running: container.running || false,
                active: container.active || false
            };
            this.showContainerDetailsModal = true;
        },
        openGroupDetails(group) {
            this.detailsGroup = {
                name: group.name,
                container: [...(group.container || [])],
                active: group.active || false
            };
            this.showGroupDetailsModal = true;
        },
        
        async deleteContainer(name) {
            const ok = await this.confirmPromise('Delete container', `Delete container "${name}"?`);
            if (!ok) return;
            try {
                const res = await fetch(`${this.apiBase}/container/${encodeURIComponent(name)}`, {
                    method: 'DELETE'
                });
                if (!res.ok) throw new Error(await res.text());
                this.containers = await res.json();
                // Refresh schedules tab because schedules targeting this container were removed server-side
                await this.loadSchedules();
                this.showSuccess('Container deleted');
            } catch (e) {
                this.showError('Failed to delete container: ' + e.message);
            }
        },
        
        async startContainer(name) {
            try {
                const res = await fetch(`${this.apiBase}/runtime/${encodeURIComponent(name)}/start`, {
                    method: 'POST'
                });
                if (!res.ok) {
                    const err = await res.json();
                    throw new Error(err.error || 'Start failed');
                }
                this.showSuccess(`Starting container "${name}" ....`);
                await this.loadContainers();
            } catch (e) {
                this.showError('Failed to start container: ' + e.message);
            }
        },
        
        async stopContainer(name) {
            try {
                const res = await fetch(`${this.apiBase}/runtime/${encodeURIComponent(name)}/stop`, {
                    method: 'POST'
                });
                if (!res.ok) {
                    const err = await res.json();
                    throw new Error(err.error || 'Stop failed');
                }
                this.showSuccess(`Stopping container "${name}" ....`);
                await this.loadContainers();
            } catch (e) {
                this.showError('Failed to stop container: ' + e.message);
            }
        },
        
        // ==================== GROUPS ====================
        async loadGroups() {
            try {
                const res = await fetch(`${this.apiBase}/groups`);
                if (!res.ok) throw new Error(await res.text());
                this.groups = await res.json();
            } catch (e) {
                this.showError('Failed to load groups: ' + e.message);
            }
        },
        
        openGroupModal(group = null) {
            if (group) {
                this.editingGroup = true;
                this.groupForm = {
                    name: group.name,
                    container: [...(group.container || [])],
                    active: group.active || false
                };
            } else {
                this.editingGroup = false;
                this.groupForm = {
                    name: '',
                    container: [],
                    active: true
                };
            }
            this.showGroupModal = true;
        },
        
        toggleGroupContainer(containerName) {
            const idx = this.groupForm.container.indexOf(containerName);
            if (idx === -1) {
                this.groupForm.container.push(containerName);
            } else {
                this.groupForm.container.splice(idx, 1);
            }
        },
        
        async saveGroup() {
            try {
                const payload = {
                    name: this.groupForm.name,
                    container: this.groupForm.container,
                    active: this.groupForm.active
                };
                
                const res = await fetch(`${this.apiBase}/group`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                });
                
                if (!res.ok) {
                    const err = await res.json();
                    throw new Error(err.error || 'Save failed');
                }
                
                this.groups = await res.json();
                this.showGroupModal = false;
                this.showSuccess('Group saved successfully');
            } catch (e) {
                this.showError('Failed to save group: ' + e.message);
            }
        },
        
        async deleteGroup(name) {
            const ok = await this.confirmPromise('Delete group', `Delete group "${name}"?`);
            if (!ok) return;
            try {
                const res = await fetch(`${this.apiBase}/group/${encodeURIComponent(name)}`, {
                    method: 'DELETE'
                });
                if (!res.ok) throw new Error(await res.text());
                this.groups = await res.json();
                // Refresh schedules tab because schedules targeting this group were removed server-side
                await this.loadSchedules();
                this.showSuccess('Group deleted');
            } catch (e) {
                this.showError('Failed to delete group: ' + e.message);
            }
        },
        
        async startGroup(name) {
            try {
                const res = await fetch(`${this.apiBase}/group/${encodeURIComponent(name)}/start`, {
                    method: 'POST'
                });
                if (!res.ok) {
                    const err = await res.json();
                    throw new Error(err.error || 'Start failed');
                }
                this.showSuccess(`Starting group "${name}" containers....`);
                await this.loadContainers();
            } catch (e) {
                this.showError('Failed to start group: ' + e.message);
            }
        },
        
        async stopGroup(name) {
            try {
                const res = await fetch(`${this.apiBase}/group/${encodeURIComponent(name)}/stop`, {
                    method: 'POST'
                });
                if (!res.ok) {
                    const err = await res.json();
                    throw new Error(err.error || 'Stop failed');
                }
                this.showSuccess(`Stopping group "${name}" containers....`);
                await this.loadContainers();
            } catch (e) {
                this.showError('Failed to stop group: ' + e.message);
            }
        },
        
        // ==================== SCHEDULES ====================
        async loadSchedules() {
            try {
                const res = await fetch(`${this.apiBase}/schedules`);
                if (!res.ok) throw new Error(await res.text());
                this.schedules = await res.json();
            } catch (e) {
                this.showError('Failed to load schedules: ' + e.message);
            }
        },
        
        openScheduleModal(schedule = null) {
            if (schedule) {
                this.editingSchedule = true;
                this.scheduleForm = {
                    id: schedule.id,
                    target: schedule.target,
                    targetType: schedule.targetType,
                    timers: (schedule.timers || []).map(t => ({
                        startTime: t.startTime,
                        stopTime: t.stopTime,
                        days: [...(t.days || [])],
                        active: t.active || false
                    }))
                };
            } else {
                this.editingSchedule = false;
                this.scheduleForm = {
                    id: this.generateId(),
                    target: (this.containers && this.containers.length > 0) ? this.containers[0].name : '',
                    targetType: 'container',
                    timers: []
                };
            }
            this.showScheduleModal = true;
        },

        // When the schedule target type changes, pick a sensible default target
        onScheduleTargetTypeChange() {
            // reset target first
            this.scheduleForm.target = '';
            if (this.scheduleForm.targetType === 'container') {
                if (this.containers && this.containers.length > 0) {
                    this.scheduleForm.target = this.containers[0].name;
                }
            } else if (this.scheduleForm.targetType === 'group') {
                if (this.groups && this.groups.length > 0) {
                    this.scheduleForm.target = this.groups[0].name;
                }
            }
        },
        
        generateId() {
            return `${Date.now()}-${Math.floor(Math.random() * 10000)}`;
        },
        
        addTimer() {
            this.scheduleForm.timers.push({
                startTime: '08:00',
                stopTime: '18:00',
                days: [1, 2, 3, 4, 5], // Mon-Fri default
                active: true
            });
        },
        
        removeTimer(idx) {
            this.scheduleForm.timers.splice(idx, 1);
        },
        
        toggleTimerDay(timerIdx, dayIdx) {
            const timer = this.scheduleForm.timers[timerIdx];
            const pos = timer.days.indexOf(dayIdx);
            if (pos === -1) {
                timer.days.push(dayIdx);
                timer.days.sort((a, b) => a - b);
            } else {
                timer.days.splice(pos, 1);
            }
        },
        
        formatDays(days) {
            if (!days || days.length === 0) return 'No days';
            if (days.length === 7) return 'Every day';
            const weekdays = [1, 2, 3, 4, 5];
            const weekend = [0, 6];
            if (weekdays.every(d => days.includes(d)) && days.length === 5) return 'Weekdays';
            if (weekend.every(d => days.includes(d)) && days.length === 2) return 'Weekend';
            return days.map(d => this.dayNames[d]).join(', ');
        },
        
        async saveSchedule() {
            try {
                // Build timers with required active field
                const timers = this.scheduleForm.timers.map(t => ({
                    startTime: t.startTime,
                    stopTime: t.stopTime,
                    days: t.days,
                    active: t.active
                }));
                
                const payload = {
                    id: this.scheduleForm.id,
                    target: this.scheduleForm.target,
                    targetType: this.scheduleForm.targetType,
                    timers: timers
                };
                
                const res = await fetch(`${this.apiBase}/schedule`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                });
                
                if (!res.ok) {
                    const err = await res.json();
                    throw new Error(err.error || 'Save failed');
                }
                
                this.schedules = await res.json();
                this.showScheduleModal = false;
                this.showSuccess('Schedule saved successfully');
            } catch (e) {
                this.showError('Failed to save schedule: ' + e.message);
            }
        },
        
        async deleteSchedule(id) {
            const ok = await this.confirmPromise('Delete schedule', `Delete schedule "${id}"?`);
            if (!ok) return;
            try {
                const res = await fetch(`${this.apiBase}/schedule/${encodeURIComponent(id)}`, {
                    method: 'DELETE'
                });
                if (!res.ok) throw new Error(await res.text());
                this.schedules = await res.json();
                this.showSuccess('Schedule deleted');
            } catch (e) {
                this.showError('Failed to delete schedule: ' + e.message);
            }
        },

        // Opens a confirm modal and returns a Promise that resolves to true if confirmed
        confirmPromise(title, message) {
            return new Promise((resolve) => {
                this.confirmTitle = title || 'Confirm';
                this.confirmMessage = message || '';
                this.showConfirmModal = true;
                this._confirmResolve = (ok) => {
                    this.showConfirmModal = false;
                    // small timeout to allow modal close transition
                    setTimeout(() => {
                        this._confirmResolve = null;
                        resolve(!!ok);
                    }, 50);
                };
            });
        },
        
        // ==================== UTILITIES ====================
        // Return true if container with given name is currently running
        isContainerRunning(name) {
            const c = this.containers.find(x => x.name === name);
            return !!(c && c.running);
        },

        // Truncate a string to a given character limit (adds '...' when truncated)
        truncate(s, limit = 20) {
            if (!s) return '';
            const n = Number(limit) || 20;
            if (s.length <= n) return s;
            if (n <= 3) return s.slice(0, n);
            return s.slice(0, n - 3) + '...';
        },
        showError(msg) {
            this.error = msg;
            this.success = '';
            setTimeout(() => this.error = '', 5000);
        },
        
        showSuccess(msg) {
            this.success = msg;
            this.error = '';
            setTimeout(() => this.success = '', 3000);
        }
    };
}
