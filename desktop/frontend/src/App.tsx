import React, { useState, useEffect, useCallback } from 'react';
import { Sidebar, TabKey } from './components/Sidebar';
import { Header } from './components/Header';
import { HomeTab } from './components/HomeTab';
import { SendTab } from './components/SendTab';
import { ReceiveTab } from './components/ReceiveTab';
import { TransfersTab } from './components/TransfersTab';
import { DevicesTab } from './components/DevicesTab';
import { DiagnosticsModal } from './components/DiagnosticsModal';
import { IncomingConsentModal, IncomingTransferOffer } from './components/IncomingConsentModal';
import { PairingModal } from './components/PairingModal';
import { SettingsModal } from './components/SettingsModal';
import { MiniTransferBar } from './components/MiniTransferBar';

import {
  GetAppState,
  GetPairingQR,
  GetSettings,
  UpdateSetting,
  ListNearbyDevices,
  ListRecentTransfers,
  ListTrustedDevices,
  PickFiles,
  PickFolder,
  RemoveDevice,
  Rescan,
  RespondIncomingTransfer,
  RunDiagnostics,
  ScanItems,
  StartSend,
} from '../wailsjs/go/main/App';
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime';
import { main, discovery, store, diag } from '../wailsjs/go/models';

interface Toast {
  message: string;
  type: 'info' | 'success' | 'error';
}

function App() {
  const [activeTab, setActiveTab] = useState<TabKey>('home');
  const [theme, setTheme] = useState<'dark' | 'light'>('dark');
  const [appState, setAppState] = useState<main.AppState | null>(null);
  const [nearbyDevices, setNearbyDevices] = useState<discovery.DiscoveredPeer[]>([]);
  const [trustedDevices, setTrustedDevices] = useState<store.TrustedDevice[]>([]);
  const [recentTransfers, setRecentTransfers] = useState<store.TransferRecord[]>([]);
  const [selectedDevice, setSelectedDevice] = useState<discovery.DiscoveredPeer | null>(null);
  const [pairingQR, setPairingQR] = useState<string>('');

  const [activeProgress, setActiveProgress] = useState<{
    bytesAcked: number;
    totalBytes: number;
    filesDone: number;
    totalFiles: number;
  } | null>(null);

  const [incomingOffer, setIncomingOffer] = useState<IncomingTransferOffer | null>(null);
  const [diagOpen, setDiagOpen] = useState<boolean>(false);
  const [pairingOpen, setPairingOpen] = useState<boolean>(false);
  const [settingsOpen, setSettingsOpen] = useState<boolean>(false);
  const [toast, setToast] = useState<Toast | null>(null);

  const showToast = (message: string, type: 'info' | 'success' | 'error' = 'info') => {
    setToast({ message, type });
    setTimeout(() => {
      setToast(null);
    }, 4500);
  };

  // Sync theme to root document
  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
    if (theme === 'dark') {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
  }, [theme]);

  const loadAppState = useCallback(async () => {
    try {
      const state = await GetAppState();
      if (state) setAppState(state);
    } catch (err) {
      console.error('Failed to load app state:', err);
    }
  }, []);

  const loadNearbyDevices = useCallback(async () => {
    try {
      const peers = await ListNearbyDevices();
      setNearbyDevices(peers || []);
    } catch (err) {
      console.error('Failed to load nearby devices:', err);
    }
  }, []);

  const loadTrustedDevices = useCallback(async () => {
    try {
      const trusted = await ListTrustedDevices();
      setTrustedDevices(trusted || []);
    } catch (err) {
      console.error('Failed to load trusted devices:', err);
    }
  }, []);

  const loadRecentTransfers = useCallback(async () => {
    try {
      const list = await ListRecentTransfers(50);
      setRecentTransfers(list || []);
    } catch (err) {
      console.error('Failed to load recent transfers:', err);
    }
  }, []);

  const loadPairingQR = useCallback(async () => {
    try {
      const qr = await GetPairingQR();
      setPairingQR(qr || '');
    } catch (err) {
      console.error('Failed to load pairing QR:', err);
    }
  }, []);

  // Initial data loading
  useEffect(() => {
    loadAppState();
    loadNearbyDevices();
    loadTrustedDevices();
    loadRecentTransfers();
    loadPairingQR();
  }, [loadAppState, loadNearbyDevices, loadTrustedDevices, loadRecentTransfers, loadPairingQR]);

  // Wails Event Listeners
  useEffect(() => {
    EventsOn('devices:updated', (peers: any) => {
      if (Array.isArray(peers)) {
        setNearbyDevices(peers);
      }
    });

    EventsOn('transfer:incoming', (req: any) => {
      console.log('Incoming transfer request:', req);
      setIncomingOffer(req);
    });

    EventsOn('transfer:progress', (p: any) => {
      setActiveProgress({
        bytesAcked: p.bytesAcked || p.bytes_acked || 0,
        totalBytes: p.totalBytes || p.total_bytes || 0,
        filesDone: p.filesDone || p.files_done || 0,
        totalFiles: p.totalFiles || p.total_files || 0,
      });
    });

    EventsOn('transfer:completed', (msg: any) => {
      showToast('Transfer completed successfully!', 'success');
      setActiveProgress(null);
      loadRecentTransfers();
      loadTrustedDevices();
    });

    EventsOn('transfer:error', (err: any) => {
      showToast(`Transfer error: ${err}`, 'error');
      setActiveProgress(null);
      loadRecentTransfers();
    });

    return () => {
      EventsOff('devices:updated');
      EventsOff('transfer:incoming');
      EventsOff('transfer:progress');
      EventsOff('transfer:completed');
      EventsOff('transfer:error');
    };
  }, [loadRecentTransfers, loadTrustedDevices]);

  // Actions
  const handleToggleTheme = () => {
    setTheme((prev) => (prev === 'dark' ? 'light' : 'dark'));
  };

  const handleToggleVisibility = async () => {
    const next = appState?.visibility === 'everyone' ? 'contacts_only' : 'everyone';
    try {
      await UpdateSetting('visibility', next);
      await loadAppState();
      showToast(`Visibility set to: ${next === 'everyone' ? 'Everyone' : 'Contacts Only'}`, 'info');
    } catch (err) {
      showToast(`Failed to update visibility: ${err}`, 'error');
    }
  };

  const handleSendToDevice = (device: discovery.DiscoveredPeer) => {
    setSelectedDevice(device);
    setActiveTab('send');
  };

  const handlePickFilesForSend = async () => {
    setActiveTab('send');
  };

  const handlePickFolderForSend = async () => {
    setActiveTab('send');
  };

  const handleRescan = async () => {
    try {
      await Rescan();
      await loadNearbyDevices();
      showToast('Beacon broadcast sent. Scanning local network...', 'info');
    } catch (err) {
      showToast(`Rescan failed: ${err}`, 'error');
    }
  };

  const handleRemoveDevice = async (fp: string) => {
    try {
      await RemoveDevice(fp);
      await loadTrustedDevices();
      showToast('Trusted device removed', 'info');
    } catch (err) {
      showToast(`Failed to remove device: ${err}`, 'error');
    }
  };

  const handleIncomingConsent = async (transferID: string, accept: boolean) => {
    try {
      await RespondIncomingTransfer(transferID, accept);
      setIncomingOffer(null);
      if (accept) {
        showToast('Transfer accepted! Ingesting incoming chunks...', 'info');
        setActiveTab('transfers');
      } else {
        showToast('Transfer rejected.', 'info');
      }
    } catch (err) {
      showToast(`Failed to respond to transfer: ${err}`, 'error');
      setIncomingOffer(null);
    }
  };

  const handlePickSaveDir = async () => {
    try {
      const folder = await PickFolder();
      if (folder) {
        await UpdateSetting('save_directory', folder);
        await loadAppState();
        showToast(`Save directory updated to ${folder}`, 'success');
      }
    } catch (err) {
      showToast(`Failed to update save directory: ${err}`, 'error');
    }
  };

  const getHeaderDetails = () => {
    switch (activeTab) {
      case 'home':
        return { title: 'Network Vault', subtitle: 'Encrypted peer-to-peer file sharing' };
      case 'send':
        return { title: 'Send Files', subtitle: 'Select files or folders and choose a recipient' };
      case 'receive':
        return { title: 'Receive Files', subtitle: 'Pair with another device or wait for transfer requests' };
      case 'transfers':
        return { title: 'Transfer History', subtitle: 'Active and completed transfer records' };
      case 'devices':
        return { title: 'Trusted Devices', subtitle: 'Mutual TLS paired devices' };
      default:
        return { title: 'LANVault', subtitle: 'Local Vault' };
    }
  };

  const { title, subtitle } = getHeaderDetails();

  return (
    <div style={{
      display: 'flex',
      height: '100vh',
      width: '100vw',
      backgroundColor: 'var(--bg-base)',
      color: 'var(--text-primary)',
      overflow: 'hidden',
    }}>
      {/* Toast Banner */}
      {toast && (
        <div style={{
          position: 'fixed',
          top: '20px',
          right: '24px',
          zIndex: 9999,
          padding: '12px 20px',
          borderRadius: 'var(--radius-control)',
          backgroundColor: toast.type === 'error' ? 'var(--danger)' : toast.type === 'success' ? 'var(--success)' : 'var(--accent-default)',
          color: '#FFFFFF',
          fontSize: '13px',
          fontWeight: 600,
          boxShadow: '0 8px 24px rgba(0, 0, 0, 0.3)',
          display: 'flex',
          alignItems: 'center',
          gap: '10px',
          animation: 'fadeIn 180ms ease forwards',
        }}>
          {toast.message}
        </div>
      )}

      {/* Left Sidebar */}
      <Sidebar
        activeTab={activeTab}
        onTabChange={setActiveTab}
        onOpenSettings={() => setSettingsOpen(true)}
        onOpenDiag={() => setDiagOpen(true)}
        theme={theme}
        onToggleTheme={handleToggleTheme}
        activeTransfersCount={activeProgress && activeProgress.totalBytes > 0 ? 1 : 0}
      />

      {/* Main Content Area */}
      <div style={{
        flex: 1,
        height: '100vh',
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
      }}>
        {/* Top Header */}
        <Header
          title={title}
          subtitle={subtitle}
          localIP={appState?.local_ips && appState.local_ips.length > 0 ? appState.local_ips[0] : '127.0.0.1'}
          visibility={appState?.visibility || 'contacts_only'}
          onOpenDiag={() => setDiagOpen(true)}
          onOpenPairing={() => setPairingOpen(true)}
          onToggleVisibility={handleToggleVisibility}
        />

        {/* Scrollable Tab Content */}
        <div style={{
          flex: 1,
          overflowY: 'auto',
          paddingBottom: activeProgress && activeTab !== 'transfers' ? '60px' : '24px',
        }}>
          {activeTab === 'home' && (
            <HomeTab
              appState={appState}
              nearbyDevices={nearbyDevices}
              recentTransfers={recentTransfers}
              onSendToDevice={handleSendToDevice}
              onPickFiles={handlePickFilesForSend}
              onPickFolder={handlePickFolderForSend}
              onShowQR={() => setPairingOpen(true)}
              onRescan={handleRescan}
              onOpenDiag={() => setDiagOpen(true)}
            />
          )}

          {activeTab === 'send' && (
            <SendTab
              nearbyDevices={nearbyDevices}
              selectedDevice={selectedDevice}
              onSelectDevice={setSelectedDevice}
              onPickFiles={PickFiles}
              onPickFolder={PickFolder}
              onScanItems={ScanItems}
              onStartTransfer={async (addr, paths) => {
                await StartSend(addr, paths);
                setActiveTab('transfers');
              }}
            />
          )}

          {activeTab === 'receive' && (
            <ReceiveTab
              appState={appState}
              pairingQR={pairingQR}
              onRefreshQR={loadPairingQR}
              onPickSaveDir={handlePickSaveDir}
              receivedTransfers={recentTransfers.filter((t) => t.direction === 'receive')}
            />
          )}

          {activeTab === 'transfers' && (
            <TransfersTab
              transfers={recentTransfers}
              activeProgress={activeProgress}
            />
          )}

          {activeTab === 'devices' && (
            <DevicesTab
              trustedDevices={trustedDevices}
              onRemoveDevice={handleRemoveDevice}
              onOpenPairing={() => setPairingOpen(true)}
            />
          )}
        </div>

        {/* Bottom Persistent Mini Progress Bar */}
        {activeTab !== 'transfers' && (
          <MiniTransferBar
            progress={activeProgress}
            onClick={() => setActiveTab('transfers')}
          />
        )}
      </div>

      {/* Modals */}
      <IncomingConsentModal
        offer={incomingOffer}
        onRespond={handleIncomingConsent}
      />

      <PairingModal
        isOpen={pairingOpen}
        onClose={() => setPairingOpen(false)}
        pairingQR={pairingQR}
        onRefreshQR={loadPairingQR}
      />

      <DiagnosticsModal
        isOpen={diagOpen}
        onClose={() => setDiagOpen(false)}
        onRunDiag={RunDiagnostics}
      />

      <SettingsModal
        isOpen={settingsOpen}
        onClose={() => setSettingsOpen(false)}
        appState={appState}
        onUpdateSetting={UpdateSetting}
        onPickFolder={PickFolder}
        onRefreshAppState={loadAppState}
      />
    </div>
  );
}

export default App;
