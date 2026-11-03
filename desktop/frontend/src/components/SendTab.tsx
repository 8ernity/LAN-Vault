import React, { useState } from 'react';
import { 
  UploadCloud, 
  FolderPlus, 
  FilePlus, 
  Trash2, 
  Send, 
  Smartphone, 
  Laptop, 
  Check, 
  Sliders 
} from 'lucide-react';
import { discovery, main, store } from '../../wailsjs/go/models';

interface SendTabProps {
  nearbyDevices: discovery.DiscoveredPeer[];
  selectedDevice: discovery.DiscoveredPeer | null;
  trustedDevices?: store.TrustedDevice[];
  onSelectDevice: (device: discovery.DiscoveredPeer) => void;
  onPickFiles: () => Promise<string[]>;
  onPickFolder: () => Promise<string>;
  onScanItems: (paths: string[]) => Promise<main.ScannedSelection>;
  onStartTransfer: (targetAddr: string, paths: string[], peerFingerprint: string) => Promise<void>;
}

export const SendTab: React.FC<SendTabProps> = ({
  nearbyDevices,
  selectedDevice,
  trustedDevices = [],
  onSelectDevice,
  onPickFiles,
  onPickFolder,
  onScanItems,
  onStartTransfer,
}) => {
  const [selectedPaths, setSelectedPaths] = useState<string[]>([]);
  const [scanResult, setScanResult] = useState<main.ScannedSelection | null>(null);
  const [manualAddr, setManualAddr] = useState<string>('');
  const [showManual, setShowManual] = useState<boolean>(false);
  const [isSending, setIsSending] = useState<boolean>(false);

  const handleAddFiles = async () => {
    try {
      const files = await onPickFiles();
      if (files && files.length > 0) {
        const merged = Array.from(new Set([...selectedPaths, ...files]));
        setSelectedPaths(merged);
        const scanned = await onScanItems(merged);
        setScanResult(scanned);
      }
    } catch (e) {
      console.error(e);
    }
  };

  const handleAddFolder = async () => {
    try {
      const folder = await onPickFolder();
      if (folder) {
        const merged = Array.from(new Set([...selectedPaths, folder]));
        setSelectedPaths(merged);
        const scanned = await onScanItems(merged);
        setScanResult(scanned);
      }
    } catch (e) {
      console.error(e);
    }
  };

  const handleRemoveItem = async (index: number) => {
    const updated = selectedPaths.filter((_, i) => i !== index);
    setSelectedPaths(updated);
    if (updated.length > 0) {
      const scanned = await onScanItems(updated);
      setScanResult(scanned);
    } else {
      setScanResult(null);
    }
  };

  const handleClearAll = () => {
    setSelectedPaths([]);
    setScanResult(null);
  };

  const handleSend = async () => {
    const target = manualAddr || (selectedDevice ? `${selectedDevice.ips[0]}:${selectedDevice.port}` : '');
    if (!target || selectedPaths.length === 0) return;
    const trusted = selectedDevice ? trustedDevices.find((t) => t.device_id === selectedDevice.device_id) : undefined;
    const peerFp = manualAddr ? '' : (trusted?.fingerprint || '');

    setIsSending(true);
    try {
      await onStartTransfer(target, selectedPaths, peerFp);
    } finally {
      setIsSending(false);
    }
  };

  const hasItems = selectedPaths.length > 0;
  const hasTarget = Boolean(selectedDevice || manualAddr);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '24px', padding: '24px 32px' }}>
      {/* Step 1: Choose Items */}
      <div style={{
        backgroundColor: 'var(--bg-surface)',
        borderRadius: 'var(--radius-card)',
        padding: '24px',
        border: '1px solid var(--border-subtle)',
        display: 'flex',
        flexDirection: 'column',
        gap: '16px'
      }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div>
            <h2 style={{ fontSize: '15px', fontWeight: 600, color: 'var(--text-primary)' }}>
              1. Choose what to send
            </h2>
            <p style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>
              Add files or entire folders. Preserves relative structure and timestamps.
            </p>
          </div>
          {hasItems && (
            <button
              onClick={handleClearAll}
              style={{
                background: 'transparent',
                border: 'none',
                color: 'var(--danger)',
                fontSize: '12px',
                fontWeight: 600,
                cursor: 'pointer'
              }}
            >
              Clear all
            </button>
          )}
        </div>

        {/* Dropzone & Buttons */}
        <div style={{
          border: '2px dashed var(--border-subtle)',
          borderRadius: 'var(--radius-control)',
          padding: '28px',
          textAlign: 'center',
          backgroundColor: 'var(--bg-base)',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          gap: '12px'
        }}>
          <UploadCloud size={32} color="var(--accent-default)" />
          <div>
            <div style={{ fontSize: '13px', fontWeight: 600, color: 'var(--text-primary)' }}>
              Select files or folders to share
            </div>
            <div style={{ fontSize: '11px', color: 'var(--text-secondary)', marginTop: '2px' }}>
              No size limits · Fast parallel transmission
            </div>
          </div>
          <div style={{ display: 'flex', gap: '10px', marginTop: '4px' }}>
            <button
              onClick={handleAddFiles}
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
              <FilePlus size={16} color="var(--accent-default)" />
              <span>Add Files</span>
            </button>
            <button
              onClick={handleAddFolder}
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
              <FolderPlus size={16} color="var(--accent-default)" />
              <span>Add Folder</span>
            </button>
          </div>
        </div>

        {/* Selected List */}
        {hasItems && (
          <div>
            <div style={{
              display: 'flex',
              justifyContent: 'space-between',
              fontSize: '12px',
              color: 'var(--text-secondary)',
              marginBottom: '8px'
            }}>
              <span>Selected paths ({selectedPaths.length})</span>
              <span>
                Total: <strong>{scanResult?.total_files || selectedPaths.length}</strong> file(s) ·{' '}
                <strong>{((scanResult?.total_bytes || 0) / (1024 * 1024)).toFixed(2)} MB</strong>
              </span>
            </div>
            <div style={{
              maxHeight: '160px',
              overflowY: 'auto',
              borderRadius: 'var(--radius-control)',
              border: '1px solid var(--border-subtle)',
              backgroundColor: 'var(--bg-base)'
            }}>
              {selectedPaths.map((p, idx) => (
                <div
                  key={idx}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '8px 12px',
                    borderBottom: idx < selectedPaths.length - 1 ? '1px solid var(--border-subtle)' : 'none',
                    fontSize: '12px'
                  }}
                >
                  <span style={{ fontFamily: 'JetBrains Mono, monospace', color: 'var(--text-primary)' }}>
                    {p}
                  </span>
                  <button
                    onClick={() => handleRemoveItem(idx)}
                    style={{ background: 'transparent', border: 'none', color: 'var(--text-secondary)', cursor: 'pointer' }}
                  >
                    <Trash2 size={14} />
                  </button>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* Step 2: Choose Device */}
      <div style={{
        backgroundColor: 'var(--bg-surface)',
        borderRadius: 'var(--radius-card)',
        padding: '24px',
        border: '1px solid var(--border-subtle)',
        display: 'flex',
        flexDirection: 'column',
        gap: '16px'
      }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div>
            <h2 style={{ fontSize: '15px', fontWeight: 600, color: 'var(--text-primary)' }}>
              2. Choose recipient device
            </h2>
            <p style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>
              Select a discovered peer or specify address manually.
            </p>
          </div>
          <button
            onClick={() => setShowManual(!showManual)}
            style={{
              background: 'transparent',
              border: 'none',
              color: 'var(--accent-default)',
              fontSize: '12px',
              fontWeight: 600,
              cursor: 'pointer'
            }}
          >
            {showManual ? 'Hide manual address' : 'Enter IP manually'}
          </button>
        </div>

        {showManual && (
          <div style={{ display: 'flex', gap: '10px' }}>
            <input
              type="text"
              placeholder="e.g. 192.168.1.50:47800"
              value={manualAddr}
              onChange={(e) => setManualAddr(e.target.value)}
              style={{
                flex: 1,
                padding: '8px 12px',
                borderRadius: 'var(--radius-control)',
                backgroundColor: 'var(--bg-base)',
                border: '1px solid var(--border-subtle)',
                color: 'var(--text-primary)',
                fontSize: '13px',
                fontFamily: 'JetBrains Mono, monospace'
              }}
            />
          </div>
        )}

        {nearbyDevices.length === 0 && !showManual ? (
          <div style={{ fontSize: '12px', color: 'var(--text-secondary)', padding: '12px 0' }}>
            No nearby devices found yet. Try clicking "Enter IP manually" above or rescan on Home tab.
          </div>
        ) : (
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 1fr))', gap: '12px' }}>
            {nearbyDevices.map((dev) => {
              const isSelected = selectedDevice?.device_id === dev.device_id && !manualAddr;
              const isAndroid = dev.platform.toLowerCase() === 'android';
              return (
                <div
                  key={dev.device_id}
                  onClick={() => {
                    onSelectDevice(dev);
                    setManualAddr('');
                  }}
                  style={{
                    padding: '12px 14px',
                    borderRadius: 'var(--radius-control)',
                    backgroundColor: isSelected ? 'rgba(76, 141, 255, 0.1)' : 'var(--bg-base)',
                    border: isSelected ? '2px solid var(--accent-default)' : '1px solid var(--border-subtle)',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    cursor: 'pointer'
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                    <div style={{ color: 'var(--accent-default)' }}>
                      {isAndroid ? <Smartphone size={18} /> : <Laptop size={18} />}
                    </div>
                    <div>
                      <div style={{ fontSize: '13px', fontWeight: 600, color: 'var(--text-primary)' }}>
                        {dev.name}
                      </div>
                      <div style={{ fontSize: '11px', color: 'var(--text-secondary)', fontFamily: 'JetBrains Mono, monospace' }}>
                        {dev.ips[0]}:{dev.port}
                      </div>
                    </div>
                  </div>
                  {isSelected && <Check size={16} color="var(--accent-default)" />}
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Step 3: Review and Send */}
      <div style={{
        backgroundColor: 'var(--bg-surface)',
        borderRadius: 'var(--radius-card)',
        padding: '24px',
        border: '1px solid var(--border-subtle)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between'
      }}>
        <div>
          <div style={{ fontSize: '14px', fontWeight: 600, color: 'var(--text-primary)' }}>
            Ready to transfer
          </div>
          <div style={{ fontSize: '12px', color: 'var(--text-secondary)', marginTop: '2px' }}>
            {hasItems ? `${scanResult?.total_files || selectedPaths.length} file(s)` : 'No files selected'} ·{' '}
            Recipient: {manualAddr || selectedDevice?.name || 'None selected'}
          </div>
        </div>

        <button
          onClick={handleSend}
          disabled={!hasItems || !hasTarget || isSending}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            backgroundColor: (!hasItems || !hasTarget || isSending) ? 'var(--text-disabled)' : 'var(--accent-default)',
            color: 'var(--accent-onAccent)',
            border: 'none',
            borderRadius: 'var(--radius-control)',
            padding: '10px 24px',
            fontSize: '14px',
            fontWeight: 600,
            cursor: (!hasItems || !hasTarget || isSending) ? 'not-allowed' : 'pointer',
            boxShadow: '0 2px 8px rgba(76, 141, 255, 0.3)'
          }}
        >
          <Send size={16} />
          <span>{isSending ? 'Sending...' : 'Send Now'}</span>
        </button>
      </div>
    </div>
  );
};
