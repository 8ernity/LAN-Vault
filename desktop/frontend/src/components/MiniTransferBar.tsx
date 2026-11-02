import React from 'react';
import { ArrowLeftRight } from 'lucide-react';

interface MiniTransferBarProps {
  progress: {
    bytesAcked: number;
    totalBytes: number;
    filesDone: number;
    totalFiles: number;
  } | null;
  onClick: () => void;
}

export const MiniTransferBar: React.FC<MiniTransferBarProps> = ({
  progress,
  onClick,
}) => {
  if (!progress || progress.totalBytes === 0) return null;

  const pct = Math.min(100, (progress.bytesAcked / progress.totalBytes) * 100);

  return (
    <div
      onClick={onClick}
      style={{
        position: 'fixed',
        bottom: 0,
        left: '220px',
        right: 0,
        height: '44px',
        backgroundColor: 'var(--bg-surface)',
        borderTop: '1px solid var(--accent-default)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        padding: '0 24px',
        cursor: 'pointer',
        zIndex: 500,
        boxShadow: '0 -2px 10px rgba(0, 0, 0, 0.2)'
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
        <ArrowLeftRight size={16} color="var(--accent-default)" />
        <span style={{ fontSize: '13px', fontWeight: 600, color: 'var(--text-primary)' }}>
          Transferring ({pct.toFixed(1)}%)
        </span>
        <span style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>
          {progress.filesDone}/{progress.totalFiles} files ·{' '}
          {(progress.bytesAcked / (1024 * 1024)).toFixed(2)} MB / {(progress.totalBytes / (1024 * 1024)).toFixed(2)} MB
        </span>
      </div>

      <div style={{ width: '180px', height: '6px', backgroundColor: 'var(--bg-raised)', borderRadius: 'var(--radius-pill)', overflow: 'hidden' }}>
        <div style={{
          width: `${pct}%`,
          height: '100%',
          backgroundColor: 'var(--accent-default)',
          transition: 'width 200ms ease'
        }} />
      </div>
    </div>
  );
};
