// WebSocket connection
let ws = null;
let reconnectTimeout = null;
let reconnectAttempts = 0;
const maxReconnectAttempts = 10;
const reconnectDelay = 3000;

// Current state
let currentAlerts = [];
let currentHasUnacknowledged = false;

// Sound playback
let soundAudio = null;
let soundInterval = null;
let soundEnabled = true;
let audioContextUnlocked = false;

function connectWebSocket() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = protocol + '//' + window.location.host + '/ws';
    
    ws = new WebSocket(wsUrl);

    ws.onopen = function() {
        console.log('WebSocket connected');
        reconnectAttempts = 0;
    };

    ws.onmessage = function(event) {
        try {
            const message = JSON.parse(event.data);
            if (message.type === 'update') {
                currentAlerts = message.alerts || [];
                currentHasUnacknowledged = message.hasUnacknowledged || false;
                updateUI();
                updateSoundStatus();
            }
        } catch (error) {
            console.error('Error parsing WebSocket message:', error);
        }
    };

    ws.onerror = function(error) {
        console.error('WebSocket error:', error);
    };

    ws.onclose = function() {
        console.log('WebSocket disconnected');
        ws = null;
        
        // Attempt to reconnect
        if (reconnectAttempts < maxReconnectAttempts) {
            reconnectAttempts++;
            reconnectTimeout = setTimeout(connectWebSocket, reconnectDelay);
            console.log('Attempting to reconnect (' + reconnectAttempts + '/' + maxReconnectAttempts + ')...');
        } else {
            console.error('Max reconnection attempts reached');
        }
    };
}

function acknowledgeAlert(alertId) {
    // Unlock audio context if needed (this is user interaction)
    if (!audioContextUnlocked) {
        if (!soundAudio) {
            initializeAudio();
        }
        if (soundAudio) {
            soundAudio.play().then(() => {
                audioContextUnlocked = true;
                soundAudio.pause();
                soundAudio.currentTime = 0;
            }).catch(err => {
                console.error('Error unlocking audio:', err);
            });
        }
    }
    
    fetch('/acknowledge?id=' + alertId, {
        method: 'POST'
    })
    .then(response => {
        if (!response.ok) {
            alert('Failed to acknowledge alert');
        }
    })
    .catch(error => {
        console.error('Error:', error);
        alert('Failed to acknowledge alert');
    });
}

function clearAlerts() {
    // Unlock audio context if needed (this is user interaction)
    if (!audioContextUnlocked) {
        if (!soundAudio) {
            initializeAudio();
        }
        if (soundAudio) {
            soundAudio.play().then(() => {
                audioContextUnlocked = true;
                soundAudio.pause();
                soundAudio.currentTime = 0;
            }).catch(err => {
                console.error('Error unlocking audio:', err);
            });
        }
    }
    
    fetch('/clear', {
        method: 'POST'
    })
    .then(response => {
        if (!response.ok) {
            alert('Failed to clear alerts');
        }
    })
    .catch(error => {
        console.error('Error:', error);
        alert('Failed to clear alerts');
    });
}

function updateUI() {
    // Update status indicator
    const statusEl = document.querySelector('.status');
    if (statusEl) {
        statusEl.className = 'status ' + (currentHasUnacknowledged ? 'active' : 'clear');
        statusEl.textContent = currentHasUnacknowledged ? '⚠️ UNACKNOWLEDGED ALERTS' : '✓ ALL CLEAR';
    }

    // Update alert list
    const alertListEl = document.querySelector('.alert-list');
    if (!alertListEl) return;

    if (currentAlerts.length === 0) {
        alertListEl.innerHTML = '<div class="empty-state">' +
            '<h2>No alerts received yet</h2>' +
            '<p>Waiting for Alertmanager to send alerts...</p>' +
            '</div>';
        return;
    }

    let html = '';
    currentAlerts.forEach(entry => {
        const alert = entry.alert || entry.Alert;
        const isAcknowledged = entry.isAcknowledged || false;
        
        // Determine status
        let statusClass = 'resolved';
        let statusText = 'Resolved';
        const alertStatus = alert.status || alert.Status;

        if (alertStatus === 'firing') {
            if (isAcknowledged) {
                statusClass = 'acknowledged';
                statusText = 'Acknowledged';
            } else {
                statusClass = 'firing';
                statusText = 'Firing';
            }
        } else if (alertStatus === 'resolved') {
            statusClass = 'resolved';
            statusText = 'Resolved';
        }

        const timestamp = entry.timestamp || entry.Timestamp;
        const timestampStr = typeof timestamp === 'string' ? timestamp : new Date(timestamp).toLocaleString();

        html += '<div class="alert-card">' +
            '<div class="alert-header">' +
            '<div>' +
            '<div class="alert-id">ID: ' + (entry.id || entry.ID) + '</div>' +
            '<div class="alert-time">' + timestampStr + '</div>' +
            '</div>' +
            '<div>' +
            '<div class="alert-status ' + statusClass + '">' + statusText + '</div>' +
            '</div>' +
            '</div>';

        if (alertStatus === 'firing' && !isAcknowledged) {
            html += '<div style="margin-bottom: 15px;">' +
                '<button class="ack-btn" onclick="acknowledgeAlert(\'' + (entry.id || entry.ID) + '\')">' +
                '✓ Acknowledge Alert' +
                '</button>' +
                '</div>';
        }

        html += '<div class="alert-item ' + statusClass + '">';

        const labels = alert.labels || alert.Labels || {};
        if (Object.keys(labels).length > 0) {
            const alertName = labels.alertname || labels.alertname;
            if (alertName) {
                html += '<div style="margin: 8px 0;"><span style="font-size: 16px; font-weight: bold; color: #333;">' + alertName + '</span></div>';
            }

            html += '<div style="margin: 8px 0;"><strong>Labels:</strong><br>';
            const labelKeys = Object.keys(labels).sort();
            labelKeys.forEach(function(k) {
                html += '<span class="label">' + k + '=' + labels[k] + '</span>';
            });
            html += '</div>';
        }

        const startsAt = alert.startsAt || alert.StartsAt;
        if (startsAt) {
            const startsAtStr = typeof startsAt === 'string' ? startsAt : new Date(startsAt).toLocaleString();
            html += '<div style="margin-top: 8px; font-size: 12px; color: #666;">Started: ' + startsAtStr + '</div>';
        }

        const endsAt = alert.endsAt || alert.EndsAt;
        if (endsAt) {
            const endsAtStr = typeof endsAt === 'string' ? endsAt : new Date(endsAt).toLocaleString();
            html += '<div style="margin-top: 4px; font-size: 12px; color: #666;">Ended: ' + endsAtStr + '</div>';
        }

        html += '</div></div>';
    });

    alertListEl.innerHTML = html;
}

function initializeAudio() {
    if (!soundAudio) {
        soundAudio = new Audio('/sound');
        soundAudio.volume = 1.0;
        soundAudio.preload = 'auto';
        
        soundAudio.addEventListener('ended', function() {
            if (soundInterval !== null && soundEnabled && currentHasUnacknowledged) {
                setTimeout(() => {
                    if (soundInterval !== null && soundEnabled && currentHasUnacknowledged) {
                        soundAudio.play().catch(err => {
                            console.error('Error playing sound after end:', err);
                        });
                    }
                }, 2000);
            }
        });
        
        soundAudio.addEventListener('error', function(e) {
            console.error('Error loading sound:', e);
            stopSoundLoop();
        });
    }
}

function updateSoundStatus() {
    if (currentHasUnacknowledged && soundEnabled && audioContextUnlocked) {
                startSoundLoop();
            } else {
                stopSoundLoop();
            }
}

function startSoundLoop() {
    if (soundInterval !== null) {
        return;
    }
    
    if (!soundAudio || !soundEnabled || !audioContextUnlocked) {
        return;
    }

    soundAudio.play().catch(err => {
        console.error('Error playing sound:', err);
    });

    soundInterval = setInterval(() => {
        if (!soundEnabled || !audioContextUnlocked || !currentHasUnacknowledged) {
            stopSoundLoop();
            return;
        }
        
        if (soundAudio.paused && soundInterval !== null) {
                    soundAudio.play().catch(err => {
                        console.error('Error restarting sound:', err);
                    });
                }
    }, 1000);
}

function stopSoundLoop() {
    if (soundInterval !== null) {
        clearInterval(soundInterval);
        soundInterval = null;
    }
    if (soundAudio && !soundAudio.paused) {
        soundAudio.pause();
        soundAudio.currentTime = 0;
    }
}

// Initialize audio
initializeAudio();

// Try to unlock audio context automatically
if (soundAudio) {
    soundAudio.play().then(() => {
        audioContextUnlocked = true;
        soundAudio.pause();
        soundAudio.currentTime = 0;
    }).catch(err => {
        console.log('Could not auto-unlock audio. Audio will unlock on next user interaction.');
        const unlockOnInteraction = function() {
            if (!audioContextUnlocked && soundAudio) {
                soundAudio.play().then(() => {
                    audioContextUnlocked = true;
                    soundAudio.pause();
                    soundAudio.currentTime = 0;
                    updateSoundStatus();
                }).catch(e => {
                    console.error('Error unlocking audio:', e);
                });
            }
            document.removeEventListener('click', unlockOnInteraction);
            document.removeEventListener('keydown', unlockOnInteraction);
        };
        document.addEventListener('click', unlockOnInteraction, { once: true });
        document.addEventListener('keydown', unlockOnInteraction, { once: true });
    });
}

// Connect WebSocket
connectWebSocket();

// Cleanup on page unload
window.addEventListener('beforeunload', function() {
    if (ws) {
        ws.close();
    }
    if (reconnectTimeout) {
        clearTimeout(reconnectTimeout);
    }
    stopSoundLoop();
});

