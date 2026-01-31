// Go variables passed to JS are expected to be defined in the HTML template.
document.addEventListener('DOMContentLoaded', () => {

    // --- New Dashboard Stats Polling ---
    const elements = {
        osType: document.getElementById('os-type'),
        cpuUsage: document.getElementById('cpu-usage'),
        memoryUsage: document.getElementById('memory-usage'),
        av1Encoder: document.getElementById('av1-encoder'),
        totalImages: document.getElementById('total-images'),
        imageUsage: document.getElementById('image-usage'),
        diskUsage: document.getElementById('disk-usage'),
        lastSnapshotTime: document.getElementById('last-snapshot-time'),
    };

    const updateUsageColor = (element, value) => {
        if (!element || value === null || value === undefined) return;
        
        element.classList.remove('text-warning', 'text-danger');

        if (value > 95) {
            element.classList.add('text-danger');
        } else if (value > 80) {
            element.classList.add('text-warning');
        }
    };

    const updateDashboard = (data) => {
        if (!data) return;

        // System Info
        if (data.system_info) {
            elements.osType.textContent = data.system_info.os_type || 'N/A';
            elements.cpuUsage.textContent = data.system_info.cpu_usage || 'Loading...';
            elements.memoryUsage.textContent = data.system_info.memory_usage || 'Loading...';
            elements.av1Encoder.textContent = data.system_info.av1_encoder || 'N/A';
            updateUsageColor(elements.cpuUsage, data.system_info.cpu_usage_raw);
            updateUsageColor(elements.memoryUsage, data.system_info.memory_usage_raw);
        }

        // Storage Statistics
        elements.totalImages.textContent = data.total_images || 'Loading...';
        elements.lastSnapshotTime.textContent = data.last_image_time || 'Loading...';
        if (data.image_size && typeof data.image_size === 'object') {
            elements.imageUsage.textContent = data.image_size.image_usage_gb || 'N/A';
            elements.diskUsage.textContent = `${data.image_size.disk_used_gb} / ${data.image_size.disk_total_gb} (${data.image_size.disk_used_percent})`;
        } else {
            elements.imageUsage.textContent = data.image_size || 'Loading...';
            elements.diskUsage.textContent = 'Loading...';
        }
    };

    const pollAndUpdateDashboard = async () => {
        try {
            const response = await fetch('/api/images');
            const data = await response.json();
            updateDashboard(data);
        } catch (error) {
            console.error('Error fetching dashboard stats:', error);
        }
    };
    
    pollAndUpdateDashboard();
    setInterval(pollAndUpdateDashboard, 5000);

    
    // --- Timelapse Card Logic ---
    const allTimelapseSelects = document.querySelectorAll('.timelapse-select');
    allTimelapseSelects.forEach(select => {
        select.addEventListener('change', (event) => {
            const newSource = event.target.value;
            const videoPlayerId = event.target.dataset.videoTarget;
            const downloadButtonId = event.target.dataset.downloadTarget;

            const videoPlayer = document.getElementById(videoPlayerId);
            const downloadButton = document.getElementById(downloadButtonId);
            const shareButton = event.target.closest('.card-body').querySelector('.share-btn');

            if (videoPlayer) {
                videoPlayer.src = newSource;
                videoPlayer.load();
            }
            if (downloadButton) {
                downloadButton.href = newSource;
            }
            if (shareButton) {
                shareButton.dataset.path = newSource;
            }
        });
    });

    // --- Gallery Logic ---
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

    // --- Logout Handler ---
    const logoutForm = document.querySelector('form[action="/logout"]');
    if (logoutForm) {
        logoutForm.addEventListener('submit', (event) => {
            event.preventDefault(); // Stop the form from submitting normally
            fetch('/logout', {
                method: 'POST',
            })
            .then(response => {
                window.location.href = '/login';
            })
            .catch(error => {
                console.error('Logout failed:', error);
                window.location.href = '/login';
            });
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
