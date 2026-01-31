document.querySelectorAll('.share-btn').forEach(button => {
    button.addEventListener('click', () => {
        const filePath = button.dataset.path;
        fetch('/share', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/x-www-form-urlencoded',
            },
            body: `filePath=${encodeURIComponent(filePath)}`
        })
        .then(response => response.json())
        .then(data => {
            if (data.shareLink) {
                const shareLinkInput = document.getElementById('shareLinkInput');
                shareLinkInput.value = data.shareLink;
                const shareLinkModal = new bootstrap.Modal(document.getElementById('shareLinkModal'));
                shareLinkModal.show();
            }
        });
    });
});

document.getElementById('copyShareLinkBtn').addEventListener('click', () => {
    const shareLinkInput = document.getElementById('shareLinkInput');
    shareLinkInput.select();
    navigator.clipboard.writeText(shareLinkInput.value);
});
