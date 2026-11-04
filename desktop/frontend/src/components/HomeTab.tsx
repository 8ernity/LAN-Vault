import React from 'react';
import { 
  Laptop, 
  Smartphone, 
  Send, 
  FolderPlus, 
  QrCode, 
  ShieldCheck, 
  ShieldAlert, 
  Clock, 
  RefreshCw, 
  CheckCircle,
  FileText
} from 'lucide-react';
import { discovery, store, main } from '../../wailsjs/go/models';

interface HomeTabProps {
  appState: main.AppState | null;
  nearbyDevices: discovery.DiscoveredPeer[];
  recentTransfers: store.TransferRecord[];
  onSendToDevice: (device: discovery.DiscoveredPeer) => void;
  onPickFiles: () => void;
  onPickFolder: () => void;
  onShowQR: () => void;
  onRescan: () => void;
  onOpenDiag: () => void;
}

export const HomeTab: React.FC<HomeTabProps> = ({
  appState,
  nearbyDevices,
  recentTransfers,
  onSendToDevice,
  onPickFiles,
  onPickFolder,
  onShowQR,
  onRescan,
  onOpenDiag,
}) => {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '24px', padding: '24px 32px' }}>
      {/* This Device Card & Quick Actions */}
      <div style={{
        display: 'grid',
        gridTemplateColumns: '1fr auto',
        gap: '20px',
        alignItems: 'center',
        backgroundColor: 'var(--bg-surface)',
        borderRadius: 'var(--radius-card)',
        padding: '20px 24px',
        border: '1px solid var(--border-subtle)',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '16px' }}>
          <div style={{
            width: '48px',
            height: '48px',
            borderRadius: 'var(--radius-control)',
            backgroundColor: 'var(--bg-raised)',
            border: '1px solid var(--border-subtle)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: 'var(--accent-default)'
          }}>
            <Laptop size={26} />
          </div>
          <div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <span style={{ fontSize: '16px', fontWeight: 600, color: 'var(--text-primary)' }}>
                {appState?.device_name || 'This PC'}
              </span>
              <span style={{
                fontSize: '11px',
                padding: '2px 8px',
                borderRadius: 'var(--radius-pill)',
                backgroundColor: 'rgba(52, 211, 153, 0.15)',
                color: 'var(--success)',
                fontWeight: 600
              }}>
                Ready to share
              </span>
            </div>
            <div style={{ display: 'flex', gap: '16px', marginTop: '4px', fontSize: '12px', color: 'var(--text-secondary)' }}>
              <span>IP: <strong style={{ fontFamily: 'JetBrains Mono, monospace', color: 'var(--text-primary)' }}>{appState?.local_ips?.[0] || '127.0.0.1'}</strong></span>
              <span>Fingerprint: <strong style={{ fontFamily: 'JetBrains Mono, monospace', color: 'var(--text-primary)' }}>{appState?.short_fingerprint || '...'}</strong></span>
            </div>
          </div>
        </div>

        {/* Quick Actions Row */}
        <div style={{ display: 'flex', gap: '10px' }}>
          <button
            onClick={onPickFiles}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '8px',
              padding: '10px 16px',
              backgroundColor: 'var(--bg-raised)',
              border: '1px solid var(--border-subtle)',
              borderRadius: 'var(--radius-control)',
              color: 'var(--text-primary)',
              fontWeight: 500,
              fontSize: '13px',
              cursor: 'pointer'
            }}
          >
            <FileText size={16} color="var(--accent-default)" />
            <span>Send Files</span>
          </button>

          <button
            onClick={onPickFolder}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '8px',
              padding: '10px 16px',
              backgroundColor: 'var(--bg-raised)',
              border: '1px solid var(--border-subtle)',
              borderRadius: 'var(--radius-control)',
              color: 'var(--text-primary)',
              fontWeight: 500,
              fontSize: '13px',
              cursor: 'pointer'
            }}
          >
            <FolderPlus size={16} color="var(--accent-default)" />
            <span>Send Folder</span>
          </button>

          <button
            onClick={onShowQR}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '8px',
              padding: '10px 16px',
              backgroundColor: 'var(--bg-raised)',
              border: '1px solid var(--border-subtle)',
              borderRadius: 'var(--radius-control)',
              color: 'var(--text-primary)',
              fontWeight: 500,
              fontSize: '13px',
              cursor: 'pointer'
            }}
          >
            <QrCode size={16} color="var(--accent-default)" />
            <span>Show QR</span>
          </button>
        </div>
      </div>

      {/* Nearby Devices Section */}
      <div>
        <div style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          marginBottom: '14px'
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <h2 style={{ fontSize: '15px', fontWeight: 600, color: 'var(--text-primary)' }}>
              Nearby Devices ({nearbyDevices.length})
            </h2>
            <span style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>
              Discovered over Wi-Fi & Hotspot
            </span>
          </div>

          <button
            onClick={onRescan}
            title="Scan for new devices"
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '6px',
              background: 'transparent',
              border: 'none',
              color: 'var(--accent-default)',
              fontSize: '12px',
              fontWeight: 600,
              cursor: 'pointer'
            }}
          >
            <RefreshCw size={14} />
            <span>Rescan</span>
          </button>
        </div>

        {nearbyDevices.length === 0 ? (
          /* Empty state */
          <div style={{
            backgroundColor: 'var(--bg-surface)',
            border: '1px dashed var(--border-subtle)',
            borderRadius: 'var(--radius-card)',
            padding: '36px',
            textAlign: 'center'
          }}>
            <div style={{
              width: '48px',
              height: '48px',
              borderRadius: '50%',
              backgroundColor: 'var(--bg-raised)',
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: 'var(--text-secondary)',
              marginBottom: '12px'
            }}>
              <RefreshCw size={24} className="animate-pulse-slow" />
            </div>
            <h3 style={{ fontSize: '14px', fontWeight: 600, color: 'var(--text-primary)' }}>
              Looking for nearby devices...
            </h3>
            <p style={{ fontSize: '12px', color: 'var(--text-secondary)', maxWidth: '420px', margin: '6px auto 16px auto' }}>
              Make sure your Android phone or other PC is connected to the same Wi-Fi network or hotspot and has LANVault open.
            </p>
            <div style={{ display: 'flex', gap: '10px', justifyContent: 'center' }}>
              <button
                onClick={onOpenDiag}
                style={{
                  padding: '8px 16px',
                  backgroundColor: 'var(--bg-raised)',
                  border: '1px solid var(--border-subtle)',
                  borderRadius: 'var(--radius-control)',
                  fontSize: '12px',
                  fontWeight: 500,
                  color: 'var(--text-primary)',
                  cursor: 'pointer'
                }}
              >
                Can't connect? Check Diagnostics
              </button>
              <button
                onClick={onShowQR}
                style={{
                  padding: '8px 16px',
                  backgroundColor: 'var(--accent-default)',
                  border: 'none',
                  borderRadius: 'var(--radius-control)',
                  fontSize: '12px',
                  fontWeight: 600,
                  color: 'var(--accent-onAccent)',
                  cursor: 'pointer'
                }}
              >
                Show Pairing QR
              </button>
            </div>
          </div>
        ) : (
          /* Grid of devices */
          <div style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
            gap: '16px'
          }}>
            {nearbyDevices.map((dev) => {
              const isAndroid = dev.platform.toLowerCase() === 'android';
              return (
                <div
                  key={dev.device_id}
                  data-device-id={dev.device_id}
                  style={{
                    backgroundColor: 'var(--bg-surface)',
                    border: '1px solid var(--border-subtle)',
                    borderRadius: 'var(--radius-card)',
                    padding: '16px',
                    display: 'flex',
                    flexDirection: 'column',
                    gap: '12px',
                    transition: 'border-color var(--motion-micro)'
                  }}
                  onMouseEnter={(e) => (e.currentTarget.style.borderColor = 'var(--accent-default)')}
                  onMouseLeave={(e) => (e.currentTarget.style.borderColor = 'var(--border-subtle)')}
                  onDragOver={(e) => {
                    e.preventDefault();
                    e.currentTarget.style.borderColor = 'var(--accent-default)';
                  }}
                  onDragLeave={(e) => {
                    e.preventDefault();
                    e.currentTarget.style.borderColor = 'var(--border-subtle)';
                  }}
                  onDrop={(e) => {
                    e.preventDefault();
                    onSendToDevice(dev);
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                      <div style={{
                        width: '36px',
                        height: '36px',
                        borderRadius: 'var(--radius-control)',
                        backgroundColor: 'var(--bg-raised)',
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        color: 'var(--accent-default)'
                      }}>
                        {isAndroid ? <Smartphone size={20} /> : <Laptop size={20} />}
                      </div>
                      <div>
                        <div style={{ fontWeight: 600, fontSize: '13px', color: 'var(--text-primary)' }}>
                          {dev.name}
                        </div>
                        <div style={{ fontSize: '11px', color: 'var(--text-secondary)', fontFamily: 'JetBrains Mono, monospace' }}>
                          {dev.ips?.[0]}:{dev.port}
                        </div>
                      </div>
                    </div>

                    <span style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: '4px',
                      fontSize: '11px',
                      padding: '2px 8px',
                      borderRadius: 'var(--radius-pill)',
                      backgroundColor: 'rgba(52, 211, 153, 0.15)',
                      color: 'var(--success)'
                    }}>
                      <span style={{ width: '6px', height: '6px', borderRadius: '50%', backgroundColor: 'var(--success)' }} />
                      Online
                    </span>
                  </div>

                  <button
                    onClick={() => onSendToDevice(dev)}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      gap: '8px',
                      width: '100%',
                      padding: '8px',
                      backgroundColor: 'var(--accent-default)',
                      color: 'var(--accent-onAccent)',
                      border: 'none',
                      borderRadius: 'var(--radius-control)',
                      fontSize: '12px',
                      fontWeight: 600,
                      cursor: 'pointer'
                    }}
                  >
                    <Send size={14} />
                    <span>Send Files</span>
                  </button>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Recent Transfers Section */}
      <div>
        <h2 style={{ fontSize: '15px', fontWeight: 600, color: 'var(--text-primary)', marginBottom: '12px' }}>
          Recent Activity
        </h2>
        {recentTransfers.length === 0 ? (
          <div style={{
            backgroundColor: 'var(--bg-surface)',
            border: '1px solid var(--border-subtle)',
            borderRadius: 'var(--radius-card)',
            padding: '24px',
            textAlign: 'center',
            color: 'var(--text-secondary)',
            fontSize: '12px'
          }}>
            No recent transfers yet. Start by sending a file or folder above!
          </div>
        ) : (
          <div style={{
            backgroundColor: 'var(--bg-surface)',
            border: '1px solid var(--border-subtle)',
            borderRadius: 'var(--radius-card)',
            overflow: 'hidden'
          }}>
            {recentTransfers.slice(0, 5).map((t, idx) => (
              <div
                key={t.transfer_id}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  padding: '12px 18px',
                  borderBottom: idx < recentTransfers.length - 1 ? '1px solid var(--border-subtle)' : 'none',
                  fontSize: '13px'
                }}
              >
                <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                  <div style={{ color: t.state === 'Completed' ? 'var(--success)' : 'var(--text-secondary)' }}>
                    {t.state === 'Completed' ? <CheckCircle size={18} /> : <Clock size={18} />}
                  </div>
                  <div>
                    <div style={{ fontWeight: 500, color: 'var(--text-primary)' }}>
                      {t.direction === 'send' ? 'Sent to' : 'Received from'} {t.peer_name}
                    </div>
                    <div style={{ fontSize: '11px', color: 'var(--text-secondary)' }}>
                      {t.total_files} file(s) · {(t.total_bytes / (1024 * 1024)).toFixed(2)} MB
                    </div>
                  </div>
                </div>

                <span style={{
                  fontSize: '11px',
                  fontWeight: 600,
                  padding: '2px 8px',
                  borderRadius: 'var(--radius-pill)',
                  backgroundColor: t.state === 'Completed' ? 'rgba(52, 211, 153, 0.15)' : 'rgba(251, 191, 36, 0.15)',
                  color: t.state === 'Completed' ? 'var(--success)' : 'var(--warning)'
                }}>
                  {t.state}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};
