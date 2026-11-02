import React from 'react';
import { Wifi, Eye, QrCode } from 'lucide-react';

interface HeaderProps {
  title: string;
  subtitle: string;
  localIP: string;
  visibility: string;
  onOpenDiag: () => void;
  onOpenPairing: () => void;
  onToggleVisibility: () => void;
}

export const Header: React.FC<HeaderProps> = ({
  title,
  subtitle,
  localIP,
  visibility,
  onOpenDiag,
  onOpenPairing,
  onToggleVisibility,
}) => {
  return (
    <header style={{
      padding: '20px 32px 16px 32px',
      borderBottom: '1px solid var(--border-subtle)',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'space-between',
      backgroundColor: 'var(--bg-base)',
      flexShrink: 0
    }}>
      <div>
        <h1 style={{ fontSize: '20px', fontWeight: 600, color: 'var(--text-primary)' }}>
          {title}
        </h1>
        <p style={{ fontSize: '12px', color: 'var(--text-secondary)', marginTop: '2px' }}>
          {subtitle}
        </p>
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
        {/* Network Chip */}
        <button
          onClick={onOpenDiag}
          title="Click to view network diagnostics"
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            backgroundColor: 'var(--bg-surface)',
            border: '1px solid var(--border-subtle)',
            borderRadius: 'var(--radius-pill)',
            padding: '6px 14px',
            fontSize: '12px',
            color: 'var(--text-primary)',
            cursor: 'pointer',
            transition: 'border-color var(--motion-micro)'
          }}
          onMouseEnter={(e) => (e.currentTarget.style.borderColor = 'var(--accent-default)')}
          onMouseLeave={(e) => (e.currentTarget.style.borderColor = 'var(--border-subtle)')}
        >
          <span style={{
            width: '8px',
            height: '8px',
            borderRadius: '50%',
            backgroundColor: 'var(--success)',
            display: 'inline-block'
          }} />
          <Wifi size={14} color="var(--text-secondary)" />
          <span style={{ fontFamily: 'JetBrains Mono, monospace' }}>
            {localIP || 'Connecting...'}
          </span>
        </button>

        {/* Visibility Chip */}
        <button
          onClick={onToggleVisibility}
          title="Toggle peer visibility"
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            backgroundColor: 'var(--bg-surface)',
            border: '1px solid var(--border-subtle)',
            borderRadius: 'var(--radius-pill)',
            padding: '6px 14px',
            fontSize: '12px',
            color: 'var(--text-secondary)',
            cursor: 'pointer'
          }}
        >
          <Eye size={14} />
          <span>{visibility}</span>
        </button>

        {/* Pair Button */}
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
            cursor: 'pointer',
            boxShadow: '0 2px 6px rgba(76, 141, 255, 0.25)'
          }}
        >
          <QrCode size={16} />
          <span>Pair Device</span>
        </button>
      </div>
    </header>
  );
};
