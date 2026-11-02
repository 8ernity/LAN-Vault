import React, { useState, useEffect } from 'react';
import { X, FolderOpen, Save, Shield, HardDrive, Eye } from 'lucide-react';
import { main } from '../../wailsjs/go/models';

interface SettingsModalProps {
  isOpen: boolean;
  onClose: () => void;
  appState: main.AppState | null;
  onUpdateSetting: (key: string, value: string) => Promise<void>;
  onPickFolder: () => Promise<string>;
  onRefreshAppState: () => Promise<void>;
}

export const SettingsModal: React.FC<SettingsModalProps> = ({
  isOpen,
  onClose,
  appState,
  onUpdateSetting,
  onPickFolder,
  onRefreshAppState,
}) => {
  const [deviceName, setDeviceName] = useState<string>('');
  const [saveDir, setSaveDir] = useState<string>('');
  const [visibility, setVisibility] = useState<string>('contacts_only');
  const [saving, setSaving] = useState<boolean>(false);
  const [savedSuccess, setSavedSuccess] = useState<boolean>(false);

  useEffect(() => {
    if (appState) {
      setDeviceName(appState.device_name || '');
      setSaveDir(appState.save_dir || '');
      setVisibility(appState.visibility || 'contacts_only');
    }
  }, [appState, isOpen]);

  if (!isOpen) return null;

  const handlePickDirectory = async () => {
    try {
      const folder = await onPickFolder();
      if (folder) {
        setSaveDir(folder);
      }
    } catch (err) {
      console.error('Failed to pick folder:', err);
    }
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      if (deviceName.trim() && deviceName !== appState?.device_name) {
        await onUpdateSetting('device_name', deviceName.trim());
      }
      if (saveDir && saveDir !== appState?.save_dir) {
        await onUpdateSetting('save_directory', saveDir);
      }
      if (visibility !== appState?.visibility) {
        await onUpdateSetting('visibility', visibility);
      }
      await onRefreshAppState();
      setSavedSuccess(true);
      setTimeout(() => {
        setSavedSuccess(false);
        onClose();
      }, 800);
    } catch (err) {
      console.error('Save failed:', err);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div style={{
      position: 'fixed',
      top: 0,
      left: 0,
      width: '100vw',
      height: '100vh',
      backgroundColor: 'rgba(0, 0, 0, 0.65)',
      backdropFilter: 'blur(8px)',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      zIndex: 1000,
    }}>
      <div style={{
        backgroundColor: 'var(--bg-surface)',
        borderRadius: 'var(--radius-dialog)',
        border: '1px solid var(--border-subtle)',
        width: '520px',
        maxWidth: '92vw',
        padding: '28px',
        display: 'flex',
        flexDirection: 'column',
        gap: '20px',
        boxShadow: '0 20px 40px rgba(0, 0, 0, 0.4)',
      }}>
        {/* Header */}
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
              color: 'var(--accent-default)',
            }}>
              <Shield size={20} />
            </div>
            <div>
              <h2 style={{ fontSize: '18px', fontWeight: 600 }}>Preferences</h2>
              <div style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>LANVault local configuration</div>
            </div>
          </div>
          <button
            onClick={onClose}
            style={{
              background: 'none',
              border: 'none',
              color: 'var(--text-secondary)',
              cursor: 'pointer',
              padding: '6px',
              borderRadius: 'var(--radius-control)',
            }}
          >
            <X size={20} />
          </button>
        </div>

        {/* Content */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
          {/* Device Name */}
          <div>
            <label style={{ fontSize: '13px', fontWeight: 500, color: 'var(--text-secondary)', display: 'block', marginBottom: '6px' }}>
              Device Display Name
            </label>
            <input
              type="text"
              value={deviceName}
              onChange={(e) => setDeviceName(e.target.value)}
              placeholder="e.g. My Laptop"
              style={{
                width: '100%',
                padding: '10px 14px',
                borderRadius: 'var(--radius-control)',
                border: '1px solid var(--border-subtle)',
                backgroundColor: 'var(--bg-base)',
                color: 'var(--text-primary)',
                fontSize: '14px',
                outline: 'none',
              }}
            />
          </div>

          {/* Save Directory */}
          <div>
            <label style={{ fontSize: '13px', fontWeight: 500, color: 'var(--text-secondary)', display: 'block', marginBottom: '6px' }}>
              Incoming Files Save Directory
            </label>
            <div style={{ display: 'flex', gap: '8px' }}>
              <input
                type="text"
                value={saveDir}
                readOnly
                style={{
                  flex: 1,
                  padding: '10px 14px',
                  borderRadius: 'var(--radius-control)',
                  border: '1px solid var(--border-subtle)',
                  backgroundColor: 'var(--bg-base)',
                  color: 'var(--text-primary)',
                  fontSize: '13px',
                  fontFamily: 'monospace',
                  outline: 'none',
                }}
              />
              <button
                onClick={handlePickDirectory}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: '6px',
                  padding: '0 14px',
                  borderRadius: 'var(--radius-control)',
                  border: '1px solid var(--border-subtle)',
                  backgroundColor: 'var(--bg-raised)',
                  color: 'var(--text-primary)',
                  cursor: 'pointer',
                  fontWeight: 500,
                  fontSize: '13px',
                }}
              >
                <FolderOpen size={16} />
                Browse
              </button>
            </div>
          </div>

          {/* Visibility */}
          <div>
            <label style={{ fontSize: '13px', fontWeight: 500, color: 'var(--text-secondary)', display: 'block', marginBottom: '6px' }}>
              Discovery Visibility
            </label>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '10px' }}>
              <button
                type="button"
                onClick={() => setVisibility('contacts_only')}
                style={{
                  padding: '12px',
                  borderRadius: 'var(--radius-control)',
                  border: visibility === 'contacts_only' ? '2px solid var(--accent-default)' : '1px solid var(--border-subtle)',
                  backgroundColor: visibility === 'contacts_only' ? 'var(--bg-raised)' : 'var(--bg-base)',
                  color: 'var(--text-primary)',
                  cursor: 'pointer',
                  textAlign: 'left',
                }}
              >
                <div style={{ fontWeight: 600, fontSize: '13px', marginBottom: '4px' }}>Contacts Only</div>
                <div style={{ fontSize: '11px', color: 'var(--text-secondary)' }}>Visible only to paired devices</div>
              </button>

              <button
                type="button"
                onClick={() => setVisibility('everyone')}
                style={{
                  padding: '12px',
                  borderRadius: 'var(--radius-control)',
                  border: visibility === 'everyone' ? '2px solid var(--accent-default)' : '1px solid var(--border-subtle)',
                  backgroundColor: visibility === 'everyone' ? 'var(--bg-raised)' : 'var(--bg-base)',
                  color: 'var(--text-primary)',
                  cursor: 'pointer',
                  textAlign: 'left',
                }}
              >
                <div style={{ fontWeight: 600, fontSize: '13px', marginBottom: '4px' }}>Everyone</div>
                <div style={{ fontSize: '11px', color: 'var(--text-secondary)' }}>Visible to all peers on local Wi-Fi</div>
              </button>
            </div>
          </div>

          {/* Static security details */}
          <div style={{
            backgroundColor: 'var(--bg-raised)',
            padding: '12px 14px',
            borderRadius: 'var(--radius-control)',
            fontSize: '12px',
            color: 'var(--text-secondary)',
            display: 'flex',
            flexDirection: 'column',
            gap: '4px',
          }}>
            <div><strong>Device ID:</strong> <span style={{ fontFamily: 'monospace' }}>{appState?.device_id}</span></div>
            <div><strong>TLS Fingerprint:</strong> <span style={{ fontFamily: 'monospace' }}>{appState?.short_fingerprint}</span></div>
            <div><strong>Listen Port:</strong> <span style={{ fontFamily: 'monospace' }}>{appState?.port || 47800} (TCP) / 47801 (UDP)</span></div>
          </div>
        </div>

        {/* Footer */}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '10px', marginTop: '8px' }}>
          <button
            onClick={onClose}
            disabled={saving}
            style={{
              padding: '8px 16px',
              borderRadius: 'var(--radius-control)',
              border: '1px solid var(--border-subtle)',
              backgroundColor: 'transparent',
              color: 'var(--text-secondary)',
              cursor: 'pointer',
              fontWeight: 500,
            }}
          >
            Cancel
          </button>
          <button
            onClick={handleSave}
            disabled={saving}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '6px',
              padding: '8px 20px',
              borderRadius: 'var(--radius-control)',
              border: 'none',
              backgroundColor: savedSuccess ? 'var(--success)' : 'var(--accent-default)',
              color: '#FFFFFF',
              cursor: saving ? 'not-allowed' : 'pointer',
              fontWeight: 600,
            }}
          >
            <Save size={16} />
            {savedSuccess ? 'Saved!' : saving ? 'Saving...' : 'Save Settings'}
          </button>
        </div>
      </div>
    </div>
  );
};
