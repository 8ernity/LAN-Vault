import React from 'react';
import { 
  ShieldCheck, 
  Trash2, 
  Smartphone, 
  Laptop, 
  QrCode 
} from 'lucide-react';
import { store } from '../../wailsjs/go/models';

interface DevicesTabProps {
  trustedDevices: store.TrustedDevice[];
  onRemoveDevice: (fp: string) => void;
  onOpenPairing: () => void;
}

export const DevicesTab: React.FC<DevicesTabProps> = ({
  trustedDevices,
  onRemoveDevice,
  onOpenPairing,
}) => {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '24px', padding: '24px 32px' }}>
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
            borderRadius: 'var(--radius-control)',
            backgroundColor: 'rgba(76, 141, 255, 0.15)',
            color: 'var(--accent-default)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center'
          }}>
            <ShieldCheck size={22} />
          </div>
          <div>
            <div style={{ fontSize: '15px', fontWeight: 600, color: 'var(--text-primary)' }}>
              Trusted Devices ({trustedDevices.length})
            </div>
            <div style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>
              Paired devices with verified, pinned certificates for mutual TLS.
            </div>
          </div>
        </div>

        <button
          onClick={onOpenPairing}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            backgroundColor: 'var(--accent-default)',
            color: 'var(--accent-onAccent)',
            border: 'none',
            borderRadius: 'var(--radius-control)',
            padding: '8px 16px',
            fontSize: '13px',
            fontWeight: 600,
            cursor: 'pointer'
          }}
        >
          <QrCode size={16} />
          <span>Pair New Device</span>
        </button>
      </div>

      {trustedDevices.length === 0 ? (
        <div style={{
          backgroundColor: 'var(--bg-surface)',
          border: '1px dashed var(--border-subtle)',
          borderRadius: 'var(--radius-card)',
          padding: '40px',
          textAlign: 'center'
        }}>
          <h3 style={{ fontSize: '14px', fontWeight: 600, color: 'var(--text-primary)' }}>
            No paired devices yet
          </h3>
          <p style={{ fontSize: '12px', color: 'var(--text-secondary)', maxWidth: '400px', margin: '6px auto 16px auto' }}>
            Pairing exchanges certificates out-of-band so future file transfers are fully authenticated and private.
          </p>
          <button
            onClick={onOpenPairing}
            style={{
              padding: '8px 18px',
              backgroundColor: 'var(--accent-default)',
              color: 'var(--accent-onAccent)',
              border: 'none',
              borderRadius: 'var(--radius-control)',
              fontSize: '13px',
              fontWeight: 600,
              cursor: 'pointer'
            }}
          >
            Pair with Phone or PC
          </button>
        </div>
      ) : (
        <div style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))',
          gap: '16px'
        }}>
          {trustedDevices.map((dev) => {
            const isAndroid = dev.platform.toLowerCase() === 'android';
            return (
              <div
                key={dev.fingerprint}
                style={{
                  backgroundColor: 'var(--bg-surface)',
                  border: '1px solid var(--border-subtle)',
                  borderRadius: 'var(--radius-card)',
                  padding: '16px 20px',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: '12px'
                }}
              >
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                    <div style={{ color: 'var(--accent-default)' }}>
                      {isAndroid ? <Smartphone size={20} /> : <Laptop size={20} />}
                    </div>
                    <div>
                      <div style={{ fontWeight: 600, fontSize: '14px', color: 'var(--text-primary)' }}>
                        {dev.name}
                      </div>
                      <div style={{ fontSize: '11px', color: 'var(--text-secondary)' }}>
                        Paired on {new Date(dev.paired_at).toLocaleDateString()}
                      </div>
                    </div>
                  </div>

                  <button
                    onClick={() => onRemoveDevice(dev.fingerprint)}
                    title="Remove trusted device"
                    style={{
                      background: 'transparent',
                      border: 'none',
                      color: 'var(--danger)',
                      cursor: 'pointer'
                    }}
                  >
                    <Trash2 size={16} />
                  </button>
                </div>

                <div style={{
                  backgroundColor: 'var(--bg-base)',
                  borderRadius: 'var(--radius-control)',
                  padding: '6px 10px',
                  fontFamily: 'JetBrains Mono, monospace',
                  fontSize: '11px',
                  color: 'var(--text-secondary)',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis'
                }}>
                  {dev.fingerprint}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
};
