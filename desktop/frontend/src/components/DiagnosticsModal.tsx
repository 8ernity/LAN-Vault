import React, { useEffect, useState } from 'react';
import { X, CheckCircle, AlertTriangle, RefreshCw } from 'lucide-react';
import { diag } from '../../wailsjs/go/models';

interface DiagnosticsModalProps {
  isOpen: boolean;
  onClose: () => void;
  onRunDiag: () => Promise<diag.DiagnosticResult[]>;
}

export const DiagnosticsModal: React.FC<DiagnosticsModalProps> = ({
  isOpen,
  onClose,
  onRunDiag,
}) => {
  const [results, setResults] = useState<diag.DiagnosticResult[]>([]);
  const [loading, setLoading] = useState<boolean>(false);

  const execute = async () => {
    setLoading(true);
    try {
      const res = await onRunDiag();
      setResults(res || []);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (isOpen) {
      execute();
    }
  }, [isOpen]);

  if (!isOpen) return null;

  return (
    <div style={{
      position: 'fixed',
      top: 0,
      left: 0,
      width: '100vw',
      height: '100vh',
      backgroundColor: 'rgba(0, 0, 0, 0.65)',
      backdropFilter: 'blur(4px)',
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
        maxWidth: '90vw',
        boxShadow: '0 8px 32px rgba(0, 0, 0, 0.4)',
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden'
      }}>
        {/* Header */}
        <div style={{
          padding: '18px 24px',
          borderBottom: '1px solid var(--border-subtle)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between'
        }}>
          <div>
            <h2 style={{ fontSize: '16px', fontWeight: 600, color: 'var(--text-primary)' }}>
              Network & System Diagnostics
            </h2>
            <p style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>
              Verify adapter status, ports, and firewall permissions.
            </p>
          </div>
          <button
            onClick={onClose}
            style={{ background: 'transparent', border: 'none', color: 'var(--text-secondary)', cursor: 'pointer' }}
          >
            <X size={18} />
          </button>
        </div>

        {/* Content */}
        <div style={{ padding: '20px 24px', display: 'flex', flexDirection: 'column', gap: '14px', maxHeight: '380px', overflowY: 'auto' }}>
          {loading ? (
            <div style={{ textAlign: 'center', padding: '24px', color: 'var(--text-secondary)' }}>
              <RefreshCw size={24} className="animate-pulse-slow" />
              <div style={{ marginTop: '8px', fontSize: '13px' }}>Running diagnostic checks...</div>
            </div>
          ) : (
            results.map((r, idx) => (
              <div
                key={idx}
                style={{
                  padding: '14px 16px',
                  borderRadius: 'var(--radius-control)',
                  backgroundColor: 'var(--bg-base)',
                  border: '1px solid var(--border-subtle)',
                  display: 'flex',
                  gap: '12px'
                }}
              >
                <div style={{ color: r.passed ? 'var(--success)' : 'var(--danger)', marginTop: '2px' }}>
                  {r.passed ? <CheckCircle size={18} /> : <AlertTriangle size={18} />}
                </div>
                <div style={{ flex: 1 }}>
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                    <span style={{ fontSize: '13px', fontWeight: 600, color: 'var(--text-primary)' }}>
                      {r.name}
                    </span>
                    <span style={{
                      fontSize: '11px',
                      fontWeight: 600,
                      padding: '2px 8px',
                      borderRadius: 'var(--radius-pill)',
                      backgroundColor: r.passed ? 'rgba(52, 211, 153, 0.15)' : 'rgba(248, 113, 113, 0.15)',
                      color: r.passed ? 'var(--success)' : 'var(--danger)'
                    }}>
                      {r.passed ? 'PASSED' : 'FAILED'}
                    </span>
                  </div>
                  <div style={{ fontSize: '12px', color: 'var(--text-secondary)', marginTop: '4px' }}>
                    {r.message}
                  </div>
                  {r.detail && (
                    <div style={{ fontSize: '11px', color: 'var(--text-disabled)', fontFamily: 'JetBrains Mono, monospace', marginTop: '4px' }}>
                      {r.detail}
                    </div>
                  )}
                </div>
              </div>
            ))
          )}
        </div>

        {/* Footer */}
        <div style={{
          padding: '14px 24px',
          borderTop: '1px solid var(--border-subtle)',
          backgroundColor: 'var(--bg-base)',
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center'
        }}>
          <button
            onClick={execute}
            disabled={loading}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '6px',
              padding: '6px 14px',
              backgroundColor: 'var(--bg-raised)',
              border: '1px solid var(--border-subtle)',
              borderRadius: 'var(--radius-control)',
              color: 'var(--text-primary)',
              fontSize: '12px',
              fontWeight: 500,
              cursor: 'pointer'
            }}
          >
            <RefreshCw size={14} />
            <span>Rerun checks</span>
          </button>

          <button
            onClick={onClose}
            style={{
              padding: '6px 16px',
              backgroundColor: 'var(--accent-default)',
              color: 'var(--accent-onAccent)',
              border: 'none',
              borderRadius: 'var(--radius-control)',
              fontSize: '12px',
              fontWeight: 600,
              cursor: 'pointer'
            }}
          >
            Done
          </button>
        </div>
      </div>
    </div>
  );
};
