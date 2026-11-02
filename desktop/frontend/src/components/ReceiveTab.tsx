import React, { useEffect, useRef, useState } from 'react';
import QRCode from 'qrcode';
import { 
  Download, 
  Folder, 
  Copy, 
  Check, 
  RefreshCw, 
  CheckCircle, 
  ExternalLink 
} from 'lucide-react';
import { main, store } from '../../wailsjs/go/models';

interface ReceiveTabProps {
  appState: main.AppState | null;
  pairingQR: string;
  onRefreshQR: () => void;
  onPickSaveDir: () => void;
  receivedTransfers: store.TransferRecord[];
}

export const ReceiveTab: React.FC<ReceiveTabProps> = ({
  appState,
  pairingQR,
  onRefreshQR,
  onPickSaveDir,
  receivedTransfers,
}) => {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const [copied, setCopied] = useState<boolean>(false);
  const [countdown, setCountdown] = useState<number>(120);

  useEffect(() => {
    if (canvasRef.current && pairingQR) {
      QRCode.toCanvas(canvasRef.current, pairingQR, {
        width: 180,
        margin: 1,
        color: {
          dark: '#000000',
          light: '#FFFFFF',
        },
      });
      setCountdown(120);
    }
  }, [pairingQR]);

  useEffect(() => {
    const timer = setInterval(() => {
      setCountdown((prev) => {
        if (prev <= 1) {
          onRefreshQR();
          return 120;
        }
        return prev - 1;
      });
    }, 1000);
    return () => clearInterval(timer);
  }, [onRefreshQR]);

  const handleCopyFP = () => {
    if (appState?.fingerprint) {
      navigator.clipboard.writeText(appState.fingerprint);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '24px', padding: '24px 32px' }}>
      {/* Top Status Card */}
      <div style={{
        backgroundColor: 'var(--bg-surface)',
        borderRadius: 'var(--radius-card)',
        padding: '20px 24px',
        border: '1px solid var(--border-subtle)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between'
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
          <div style={{
            width: '44px',
            height: '44px',
            borderRadius: '50%',
            backgroundColor: 'rgba(52, 211, 153, 0.15)',
            color: 'var(--success)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center'
          }}>
            <Download size={22} />
          </div>
          <div>
            <div style={{ fontSize: '15px', fontWeight: 600, color: 'var(--text-primary)' }}>
              Ready to receive files
            </div>
            <div style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>
              Visible to nearby devices as: <strong style={{ color: 'var(--text-primary)' }}>{appState?.device_name}</strong>
            </div>
          </div>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          <span style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>
            Visibility:
          </span>
          <span style={{
            fontSize: '12px',
            fontWeight: 600,
            padding: '4px 10px',
            borderRadius: 'var(--radius-pill)',
            backgroundColor: 'var(--bg-raised)',
            color: 'var(--accent-default)'
          }}>
            {appState?.visibility || 'Everyone nearby'}
          </span>
        </div>
      </div>

      {/* Grid: Pairing QR & Device Identity */}
      <div style={{ display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '20px' }}>
        {/* Pairing QR Card */}
        <div style={{
          backgroundColor: 'var(--bg-surface)',
          borderRadius: 'var(--radius-card)',
          padding: '24px',
          border: '1px solid var(--border-subtle)',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          gap: '14px',
          width: '240px'
        }}>
          <div style={{ fontWeight: 600, fontSize: '13px', color: 'var(--text-primary)' }}>
            Scan to Pair
          </div>

          <div style={{
            padding: '8px',
            backgroundColor: '#FFFFFF',
            borderRadius: 'var(--radius-control)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            boxShadow: '0 2px 10px rgba(0,0,0,0.1)'
          }}>
            <canvas ref={canvasRef} />
          </div>

          <div style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            width: '100%',
            fontSize: '11px',
            color: 'var(--text-secondary)'
          }}>
            <span>Expires in {countdown}s</span>
            <button
              onClick={onRefreshQR}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '4px',
                background: 'transparent',
                border: 'none',
                color: 'var(--accent-default)',
                fontSize: '11px',
                fontWeight: 600,
                cursor: 'pointer'
              }}
            >
              <RefreshCw size={12} />
              <span>Refresh</span>
            </button>
          </div>
        </div>

        {/* Identity & Save Location Details */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
          {/* Identity details */}
          <div style={{
            backgroundColor: 'var(--bg-surface)',
            borderRadius: 'var(--radius-card)',
            padding: '24px',
            border: '1px solid var(--border-subtle)',
            display: 'flex',
            flexDirection: 'column',
            gap: '14px'
          }}>
            <h3 style={{ fontSize: '14px', fontWeight: 600, color: 'var(--text-primary)' }}>
              Device Identity & Pinned Certificate
            </h3>

            <div>
              <div style={{ fontSize: '11px', color: 'var(--text-secondary)', marginBottom: '4px' }}>
                Full SHA-256 TLS Fingerprint
              </div>
              <div style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                padding: '8px 12px',
                backgroundColor: 'var(--bg-base)',
                borderRadius: 'var(--radius-control)',
                border: '1px solid var(--border-subtle)',
                fontFamily: 'JetBrains Mono, monospace',
                fontSize: '11px',
                color: 'var(--text-primary)'
              }}>
                <span style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>
                  {appState?.fingerprint || '...'}
                </span>
                <button
                  onClick={handleCopyFP}
                  title="Copy fingerprint"
                  style={{ background: 'transparent', border: 'none', color: 'var(--accent-default)', cursor: 'pointer', marginLeft: '8px' }}
                >
                  {copied ? <Check size={14} color="var(--success)" /> : <Copy size={14} />}
                </button>
              </div>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
              <div>
                <div style={{ fontSize: '11px', color: 'var(--text-secondary)' }}>Short Fingerprint</div>
                <div style={{ fontSize: '13px', fontWeight: 600, fontFamily: 'JetBrains Mono, monospace', marginTop: '2px' }}>
                  {appState?.short_fingerprint || '...'}
                </div>
              </div>

              <div>
                <div style={{ fontSize: '11px', color: 'var(--text-secondary)' }}>Port</div>
                <div style={{ fontSize: '13px', fontWeight: 600, fontFamily: 'JetBrains Mono, monospace', marginTop: '2px' }}>
                  {appState?.port || 47800} (TLS 1.3)
                </div>
              </div>
            </div>
          </div>

          {/* Save Location Row */}
          <div style={{
            backgroundColor: 'var(--bg-surface)',
            borderRadius: 'var(--radius-card)',
            padding: '20px 24px',
            border: '1px solid var(--border-subtle)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between'
          }}>
            <div>
              <div style={{ fontSize: '13px', fontWeight: 600, color: 'var(--text-primary)' }}>
                Save received files to
              </div>
              <div style={{ fontSize: '12px', color: 'var(--text-secondary)', fontFamily: 'JetBrains Mono, monospace', marginTop: '2px' }}>
                {appState?.save_dir || 'Default Downloads folder'}
              </div>
            </div>

            <button
              onClick={onPickSaveDir}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '8px',
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
              <Folder size={14} color="var(--accent-default)" />
              <span>Change Folder</span>
            </button>
          </div>
        </div>
      </div>

      {/* Recently Received Files */}
      <div>
        <h3 style={{ fontSize: '15px', fontWeight: 600, color: 'var(--text-primary)', marginBottom: '12px' }}>
          Received Transfers
        </h3>
        {receivedTransfers.length === 0 ? (
          <div style={{
            backgroundColor: 'var(--bg-surface)',
            border: '1px solid var(--border-subtle)',
            borderRadius: 'var(--radius-card)',
            padding: '24px',
            textAlign: 'center',
            color: 'var(--text-secondary)',
            fontSize: '12px'
          }}>
            No incoming files received yet.
          </div>
        ) : (
          <div style={{
            backgroundColor: 'var(--bg-surface)',
            border: '1px solid var(--border-subtle)',
            borderRadius: 'var(--radius-card)',
            overflow: 'hidden'
          }}>
            {receivedTransfers.map((t, idx) => (
              <div
                key={t.transfer_id}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  padding: '12px 18px',
                  borderBottom: idx < receivedTransfers.length - 1 ? '1px solid var(--border-subtle)' : 'none',
                  fontSize: '13px'
                }}
              >
                <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                  <CheckCircle size={18} color="var(--success)" />
                  <div>
                    <div style={{ fontWeight: 500, color: 'var(--text-primary)' }}>
                      From {t.peer_name}
                    </div>
                    <div style={{ fontSize: '11px', color: 'var(--text-secondary)' }}>
                      {t.completed_files} file(s) · {(t.completed_bytes / (1024 * 1024)).toFixed(2)} MB
                    </div>
                  </div>
                </div>

                <span style={{
                  fontSize: '11px',
                  fontWeight: 600,
                  padding: '2px 8px',
                  borderRadius: 'var(--radius-pill)',
                  backgroundColor: 'rgba(52, 211, 153, 0.15)',
                  color: 'var(--success)'
                }}>
                  Saved
                </span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};
