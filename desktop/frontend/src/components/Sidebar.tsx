import React from 'react';
import { 
  Shield, 
  LayoutDashboard, 
  Send, 
  Download, 
  ArrowLeftRight, 
  ShieldCheck, 
  Settings, 
  Activity, 
  Sun, 
  Moon 
} from 'lucide-react';

export type TabKey = 'home' | 'send' | 'receive' | 'transfers' | 'devices';

interface SidebarProps {
  activeTab: TabKey;
  onTabChange: (tab: TabKey) => void;
  onOpenSettings: () => void;
  onOpenDiag: () => void;
  theme: 'dark' | 'light';
  onToggleTheme: () => void;
  activeTransfersCount: number;
}

export const Sidebar: React.FC<SidebarProps> = ({
  activeTab,
  onTabChange,
  onOpenSettings,
  onOpenDiag,
  theme,
  onToggleTheme,
  activeTransfersCount,
}) => {
  const tabs = [
    { key: 'home' as TabKey, label: 'Home', icon: LayoutDashboard },
    { key: 'send' as TabKey, label: 'Send', icon: Send },
    { key: 'receive' as TabKey, label: 'Receive', icon: Download },
    { 
      key: 'transfers' as TabKey, 
      label: 'Transfers', 
      icon: ArrowLeftRight, 
      badge: activeTransfersCount > 0 ? activeTransfersCount : undefined 
    },
    { key: 'devices' as TabKey, label: 'Devices', icon: ShieldCheck },
  ];

  return (
    <aside style={{
      width: '220px',
      height: '100vh',
      backgroundColor: 'var(--bg-surface)',
      borderRight: '1px solid var(--border-subtle)',
      display: 'flex',
      flexDirection: 'column',
      padding: '16px 12px',
      flexShrink: 0,
      userSelect: 'none'
    }}>
      {/* Brand */}
      <div style={{
        display: 'flex',
        alignItems: 'center',
        gap: '10px',
        padding: '8px 12px 20px 12px',
      }}>
        <div style={{
          width: '32px',
          height: '32px',
          borderRadius: 'var(--radius-control)',
          backgroundColor: 'var(--accent-default)',
          color: 'var(--accent-onAccent)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          boxShadow: '0 2px 8px rgba(76, 141, 255, 0.3)'
        }}>
          <Shield size={18} />
        </div>
        <div>
          <div style={{ fontWeight: 700, fontSize: '16px', letterSpacing: '-0.3px' }}>
            LANVault
          </div>
          <div style={{ fontSize: '11px', color: 'var(--text-secondary)' }}>
            v1.0 · Wire v1.1
          </div>
        </div>
      </div>

      {/* Navigation tabs */}
      <nav style={{ display: 'flex', flexDirection: 'column', gap: '4px', flex: 1 }}>
        {tabs.map((tab) => {
          const Icon = tab.icon;
          const isActive = activeTab === tab.key;
          return (
            <button
              key={tab.key}
              onClick={() => onTabChange(tab.key)}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '12px',
                padding: '10px 12px',
                borderRadius: 'var(--radius-control)',
                border: 'none',
                background: isActive ? 'var(--bg-raised)' : 'transparent',
                color: isActive ? 'var(--accent-default)' : 'var(--text-secondary)',
                fontWeight: isActive ? 600 : 500,
                fontSize: '13px',
                cursor: 'pointer',
                transition: 'all var(--motion-micro) var(--motion-easing)',
                textAlign: 'left',
                position: 'relative'
              }}
              onMouseEnter={(e) => {
                if (!isActive) (e.currentTarget as HTMLElement).style.backgroundColor = 'var(--bg-raised)';
              }}
              onMouseLeave={(e) => {
                if (!isActive) (e.currentTarget as HTMLElement).style.backgroundColor = 'transparent';
              }}
            >
              <Icon size={18} />
              <span style={{ flex: 1 }}>{tab.label}</span>
              {tab.badge !== undefined && (
                <span style={{
                  backgroundColor: 'var(--accent-default)',
                  color: 'var(--accent-onAccent)',
                  fontSize: '10px',
                  fontWeight: 700,
                  padding: '2px 6px',
                  borderRadius: 'var(--radius-pill)',
                }}>
                  {tab.badge}
                </span>
              )}
            </button>
          );
        })}
      </nav>

      {/* Bottom Actions */}
      <div style={{
        borderTop: '1px solid var(--border-subtle)',
        paddingTop: '12px',
        display: 'flex',
        flexDirection: 'column',
        gap: '4px'
      }}>
        <button
          onClick={onOpenDiag}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '10px',
            padding: '8px 12px',
            borderRadius: 'var(--radius-control)',
            border: 'none',
            background: 'transparent',
            color: 'var(--text-secondary)',
            fontSize: '13px',
            cursor: 'pointer'
          }}
        >
          <Activity size={16} />
          <span>Diagnostics</span>
        </button>

        <button
          onClick={onOpenSettings}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '10px',
            padding: '8px 12px',
            borderRadius: 'var(--radius-control)',
            border: 'none',
            background: 'transparent',
            color: 'var(--text-secondary)',
            fontSize: '13px',
            cursor: 'pointer'
          }}
        >
          <Settings size={16} />
          <span>Settings</span>
        </button>

        <button
          onClick={onToggleTheme}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '10px',
            padding: '8px 12px',
            borderRadius: 'var(--radius-control)',
            border: 'none',
            background: 'transparent',
            color: 'var(--text-secondary)',
            fontSize: '13px',
            cursor: 'pointer'
          }}
        >
          {theme === 'dark' ? <Sun size={16} /> : <Moon size={16} />}
          <span>{theme === 'dark' ? 'Light Theme' : 'Dark Theme'}</span>
        </button>
      </div>
    </aside>
  );
};
