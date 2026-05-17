// Go variables (availableDates, defaultDate, initialGalleryData) are defined in index.html.
document.addEventListener('DOMContentLoaded', () => {

    // --- Player initialisation (Video.js) ---
    const vjsInstances = new Map();

    function mimeForSrc(src) {
        if (src.endsWith('.m3u8')) return 'application/x-mpegURL';
        if (src.endsWith('.mp4'))  return 'video/mp4';
        return 'video/webm';
    }

    function initPlayer(type) {
        const el = document.getElementById(`video-${type}`);
        if (!el) return;

        const srcEl = el.querySelector('source');
        const src   = srcEl ? srcEl.getAttribute('src') : '';
        const isHLS = src.endsWith('.m3u8');

        const player = videojs(el, {
            controls: true,
            preload: 'none',
            fluid: true,
            playbackRates: [0.5, 0.75, 1, 1.25, 1.5, 2],
            html5: {
                vhs: {
                    overrideNative: !videojs.browser.IS_SAFARI,
                    // Never cap quality to the player element's pixel dimensions.
                    // Without this, VHS refuses to serve 4K into a sub-4K player box,
                    // and manual quality changes to source are silently overridden.
                    limitRenditionByPlayerDimensions: false,
                    // Start with a high bandwidth estimate (16 Mbps) so VHS immediately
                    // selects the highest available quality level rather than ramping up
                    // from the lowest. On a LAN the first real measurement confirms this.
                    bandwidth: 16000000,
                },
            },
        });

        if (isHLS) {
            // qualityLevels() exposes the HLS manifest levels; hlsQualitySelector
            // adds a gear button to the control bar with Auto + per-level options.
            player.qualityLevels();
            player.hlsQualitySelector({ displayCurrentQuality: true });
        }

        vjsInstances.set(type, player);
    }

    ['Daily', 'Weekly', 'Monthly', 'Yearly'].forEach(type => initPlayer(type));

    // --- Dashboard stats polling ---
    const elements = {
        osType:           document.getElementById('os-type'),
        cpuUsage:         document.getElementById('cpu-usage'),
        memoryUsage:      document.getElementById('memory-usage'),
        av1Encoder:       document.getElementById('av1-encoder'),
        totalImages:      document.getElementById('total-images'),
        imageUsage:       document.getElementById('image-usage'),
        diskUsage:        document.getElementById('disk-usage'),
        lastSnapshotTime: document.getElementById('last-snapshot-time'),
    };

    const updateUsageColor = (element, value) => {
        if (!element || value == null) return;
        element.classList.remove('text-warning', 'text-danger');
        if (value > 95)      element.classList.add('text-danger');
        else if (value > 80) element.classList.add('text-warning');
    };

    const updateDashboard = (data) => {
        if (!data) return;
        if (data.system_info) {
            elements.osType.textContent      = data.system_info.os_type    || 'N/A';
            elements.cpuUsage.textContent    = data.system_info.cpu_usage  || 'Loading...';
            elements.memoryUsage.textContent = data.system_info.memory_usage || 'Loading...';
            elements.av1Encoder.textContent  = data.system_info.av1_encoder || 'N/A';
            updateUsageColor(elements.cpuUsage,    data.system_info.cpu_usage_raw);
            updateUsageColor(elements.memoryUsage, data.system_info.memory_usage_raw);
        }
        elements.totalImages.textContent      = data.total_images    || 'Loading...';
        elements.lastSnapshotTime.textContent = data.last_image_time || 'Loading...';
        if (data.image_size && typeof data.image_size === 'object') {
            elements.imageUsage.textContent = data.image_size.image_usage_gb || 'N/A';
            elements.diskUsage.textContent  = `${data.image_size.disk_used_gb} / ${data.image_size.disk_total_gb} (${data.image_size.disk_used_percent})`;
        } else {
            elements.imageUsage.textContent = data.image_size || 'Loading...';
            elements.diskUsage.textContent  = 'Loading...';
        }
    };

    const pollAndUpdateDashboard = async () => {
        try {
            const response = await fetch('/api/images');
            const data     = await response.json();
            updateDashboard(data);
        } catch (error) {
            console.error('Error fetching dashboard stats:', error);
        }
    };
    pollAndUpdateDashboard();
    setInterval(pollAndUpdateDashboard, 5000);

    // --- Timelapse select handlers ---
    document.querySelectorAll('.timelapse-select').forEach(select => {
        select.addEventListener('change', (event) => {
            const newSrc        = event.target.value;
            const selectedOption = event.target.options[event.target.selectedIndex];
            const fmt           = selectedOption.dataset.format || '';
            const videoPlayerId = event.target.dataset.videoTarget;
            const downloadBtnId = event.target.dataset.downloadTarget;
            const typeName      = videoPlayerId.replace('video-', '');
            const player        = vjsInstances.get(typeName);

            if (player) {
                // Video.js handles format switching (HLS ↔ mp4 ↔ webm) natively
                player.src({ src: newSrc, type: mimeForSrc(newSrc) });
            }

            const downloadBtn = document.getElementById(downloadBtnId);
            if (downloadBtn) {
                downloadBtn.href = newSrc;
                downloadBtn.style.display = fmt === 'hls' ? 'none' : '';
            }
            const shareBtn = event.target.closest('.card-body').querySelector('.share-btn');
            if (shareBtn) shareBtn.dataset.path = newSrc;
        });
    });

    // --- Gallery logic ---
    const dateSelect = document.getElementById('date-select');
    if (dateSelect) {
        if (typeof availableDates !== 'undefined' && availableDates) {
            availableDates.forEach(dateObj => {
                const option       = document.createElement('option');
                option.value       = dateObj.value;
                option.textContent = dateObj.display;
                if (dateObj.value === defaultDate) option.selected = true;
                dateSelect.appendChild(option);
            });
        }
        if (typeof initialGalleryData !== 'undefined' && initialGalleryData) {
            const sel = dateSelect.options[dateSelect.selectedIndex];
            renderGallery(initialGalleryData, sel ? sel.textContent : defaultDate);
        }
        dateSelect.addEventListener('change', (event) => {
            const sel = event.target.options[event.target.selectedIndex];
            fetchGallery(event.target.value, sel.textContent);
        });
    }

    // --- Logout handler ---
    const logoutForm = document.querySelector('form[action="/logout"]');
    if (logoutForm) {
        logoutForm.addEventListener('submit', (event) => {
            event.preventDefault();
            fetch('/logout', { method: 'POST' })
                .then(() => { window.location.href = '/login'; })
                .catch(() => { window.location.href = '/login'; });
        });
    }
});

function renderGallery(galleryData, displayDate) {
    const galleryGrid         = document.getElementById('gallery-grid');
    const currentGalleryDateEl = document.getElementById('current-gallery-date');
    if (!galleryGrid || !currentGalleryDateEl) return;

    galleryGrid.innerHTML = '';
    currentGalleryDateEl.textContent = displayDate;

    galleryData.forEach(item => {
        const col       = document.createElement('div');
        col.className   = 'col text-center';
        let content     = '';
        if (item.available === "true") {
            content = `
                <div class="gallery-image-container">
                    <a href="${item.url}" target="_blank">
                        <img src="${item.url}" class="gallery-image" alt="Snapshot at ${item.time}">
                    </a>
                </div>`;
        } else {
            content = `
                <div class="gallery-image-container d-flex align-items-center justify-content-center">
                    <i class="fas fa-camera-slash me-1 text-muted"></i>
                    <span class="no-capture">No Snap</span>
                </div>`;
        }
        col.innerHTML = `<small class="text-secondary">${item.time}</small>${content}`;
        galleryGrid.appendChild(col);
    });
}

async function fetchGallery(date, displayDate) {
    const galleryGrid = document.getElementById('gallery-grid');
    const galleryInfo = document.getElementById('gallery-info');
    if (!galleryGrid || !galleryInfo) return;

    galleryGrid.innerHTML = '<div class="text-center text-primary-highlight py-4"><i class="fas fa-sync fa-spin me-2"></i> Loading Gallery...</div>';
    try {
        const response = await fetch(`/api/gallery?date=${date}`);
        if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
        const data = await response.json();
        galleryInfo.innerHTML = `Displaying hourly snapshots for <span class="text-primary-highlight" id="current-gallery-date">${displayDate}</span>.`;
        renderGallery(data.images, displayDate);
    } catch (error) {
        console.error('Error fetching gallery data:', error);
        galleryGrid.innerHTML = '<div class="alert alert-danger">Failed to load gallery for this date.</div>';
    }
}
