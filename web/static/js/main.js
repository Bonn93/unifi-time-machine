// Go variables passed to JS
// These are expected to be defined in the HTML template, e.g.:
// const availableDates = JSON.parse('{{js .AvailableDates}}');
// const defaultDate = "{{.DefaultGalleryDate}}";
// const initialGalleryData = JSON.parse('{{js .DefaultGalleryImages}}');

document.addEventListener('DOMContentLoaded', () => {

    // --- System Stats Polling ---
    const cpuUsageEl = document.getElementById('cpu-usage');
    const memoryUsageEl = document.getElementById('memory-usage');

    async function fetchSystemStats() {
        try {
            const response = await fetch('/api/system-stats');
            const data = await response.json();
            if (cpuUsageEl) cpuUsageEl.textContent = data.cpu_usage;
            if (memoryUsageEl) memoryUsageEl.textContent = data.memory_usage;
        } catch (error) {
            console.error('Error fetching system stats:', error);
        }
    }

    // Fetch stats immediately and then every 5 seconds
    if (cpuUsageEl && memoryUsageEl) {
        fetchSystemStats();
        setInterval(fetchSystemStats, 5000);
    }
    
    // --- New Timelapse Card Logic ---
    const allTimelapseSelects = document.querySelectorAll('.timelapse-select');
    allTimelapseSelects.forEach(select => {
        select.addEventListener('change', (event) => {
            const newSource = event.target.value;
            const videoPlayerId = event.target.dataset.videoTarget;
            const downloadButtonId = event.target.dataset.downloadTarget;

            const videoPlayer = document.getElementById(videoPlayerId);
            const downloadButton = document.getElementById(downloadButtonId);

            if (videoPlayer) {
                videoPlayer.src = newSource;
                videoPlayer.load();
            }
            if (downloadButton) {
                downloadButton.href = newSource;
            }
        });
    });

    // --- Gallery Logic (Unchanged) ---
    const dateSelect = document.getElementById('date-select');
    if (dateSelect) {
        if (typeof availableDates !== 'undefined' && availableDates) {
            availableDates.forEach(date => {
                const option = document.createElement('option');
                option.value = date;
                option.textContent = date;
                if (date === defaultDate) {
                    option.selected = true;
                }
                dateSelect.appendChild(option);
            });
        }

        if (typeof initialGalleryData !== 'undefined' && initialGalleryData) {
            renderGallery(initialGalleryData, defaultDate);
        }

        dateSelect.addEventListener('change', (event) => {
            fetchGallery(event.target.value);
        });
    }
});

// Helper function to render the gallery grid
function renderGallery(galleryData, date) {
    const galleryGrid = document.getElementById('gallery-grid');
    const currentGalleryDateEl = document.getElementById('current-gallery-date');

    if (!galleryGrid || !currentGalleryDateEl) return;

    galleryGrid.innerHTML = '';
    currentGalleryDateEl.textContent = date;
    
    galleryData.forEach(item => {
        const col = document.createElement('div');
        col.className = 'col text-center';

        let content = '';
        if (item.available === "true") {
            content = `
                <div class="gallery-image-container">
                    <a href="${item.url}" target="_blank">
                        <img src="${item.url}" class="gallery-image" alt="Snapshot at ${item.time}">
                    </a>
                </div>
            `;
        } else {
            content = `
                <div class="gallery-image-container d-flex align-items-center justify-content-center">
                    <i class="fas fa-camera-slash me-1 text-muted"></i>
                    <span class="no-capture">No Snap</span>
                </div>
            `;
        }

        col.innerHTML = `<small class="text-secondary">${item.time}</small>${content}`;
        galleryGrid.appendChild(col);
    });
}

// Function to fetch new gallery data
async function fetchGallery(date) {
    const galleryGrid = document.getElementById('gallery-grid');
    const galleryInfo = document.getElementById('gallery-info');

    if (!galleryGrid || !galleryInfo) return;

    galleryGrid.innerHTML = '<div class="text-center text-primary-highlight py-4"><i class="fas fa-sync fa-spin me-2"></i> Loading Gallery...</div>';
    try {
        const response = await fetch(`/api/gallery?date=${date}`);
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        const data = await response.json();
        
        galleryInfo.innerHTML = `Displaying hourly snapshots for <span class="text-primary-highlight" id="current-gallery-date">${data.date}</span>.`;
        renderGallery(data.images, data.date);
    } catch (error) {
        console.error('Error fetching gallery data:', error);
        galleryGrid.innerHTML = '<div class="alert alert-danger">Failed to load gallery for this date.</div>';
    }
}
