import React from 'react';
import { 
  ArrowLeftRight, 
  CheckCircle, 
  AlertCircle, 
  Clock, 
  FileCheck 
} from 'lucide-react';
import { store } from '../../wailsjs/go/models';

interface TransfersTabProps {
  transfers: store.TransferRecord[];
  activeProgress?: {
    bytesAcked: number;
    totalBytes: number;
    filesDone: number;
    totalFiles: number;
  } | null;
}

export const TransfersTab: React.FC<TransfersTabProps> = ({
  transfers,
  activeProgress,
}) => {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '24px', padding: '24px 32px' }}>
      {/* Active Transfer Card */}
      {activeProgress && activeProgress.totalBytes > 0 && (
        <div style={{
          backgroundColor: 'var(--bg-surface)',
          borderRadius: 'var(--radius-card)',
          padding: '24px',
          border: '1px solid var(--accent-default)',
          boxShadow: '0 4px 16px rgba(76, 141, 255, 0.15)',
          display: 'flex',
          flexDirection: 'column',
          gap: '14px'
        }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
              <ArrowLeftRight size={20} color="var(--accent-default)" />
              <span style={{ fontSize: '15px', fontWeight: 600, color: 'var(--text-primary)' }}>
                Transfer in progress
              </span>
            </div>
            <span style={{
              fontSize: '12px',
              fontWeight: 700,
              color: 'var(--accent-default)',
              fontFamily: 'JetBrains Mono, monospace'
            }}>
              {((activeProgress.bytesAcked / activeProgress.totalBytes) * 100).toFixed(1)}%
            </span>
          </div>

          {/* Progress bar */}
          <div style={{
            height: '8px',
            backgroundColor: 'var(--bg-raised)',
            borderRadius: 'var(--radius-pill)',
            overflow: 'hidden'
          }}>
            <div style={{
              height: '100%',
              width: `${Math.min(100, (activeProgress.bytesAcked / activeProgress.totalBytes) * 100)}%`,
              backgroundColor: 'var(--accent-default)',
              borderRadius: 'var(--radius-pill)',
              transition: 'width 200ms ease'
            }} />
          </div>

          <div style={{
            display: 'flex',
            justifyContent: 'space-between',
            fontSize: '12px',
            color: 'var(--text-secondary)'
          }}>
            <span>
              {(activeProgress.bytesAcked / (1024 * 1024)).toFixed(2)} MB of{' '}
              {(activeProgress.totalBytes / (1024 * 1024)).toFixed(2)} MB transferred
            </span>
            <span>
              {activeProgress.filesDone} of {activeProgress.totalFiles} files completed
            </span>
          </div>
        </div>
      )}

      {/* History table */}
      <div>
        <h2 style={{ fontSize: '16px', fontWeight: 600, color: 'var(--text-primary)', marginBottom: '14px' }}>
          All Transfers History
        </h2>

        {transfers.length === 0 ? (
          <div style={{
            backgroundColor: 'var(--bg-surface)',
            border: '1px solid var(--border-subtle)',
            borderRadius: 'var(--radius-card)',
            padding: '36px',
            textAlign: 'center',
            color: 'var(--text-secondary)',
            fontSize: '13px'
          }}>
            No transfer records stored yet.
          </div>
        ) : (
          <div style={{
            backgroundColor: 'var(--bg-surface)',
            border: '1px solid var(--border-subtle)',
            borderRadius: 'var(--radius-card)',
            overflow: 'hidden'
          }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', textAlign: 'left', fontSize: '13px' }}>
              <thead>
                <tr style={{
                  borderBottom: '1px solid var(--border-subtle)',
                  color: 'var(--text-secondary)',
                  fontSize: '11px',
                  textTransform: 'uppercase',
                  letterSpacing: '0.5px'
                }}>
                  <th style={{ padding: '12px 18px' }}>Direction</th>
                  <th style={{ padding: '12px 18px' }}>Peer</th>
                  <th style={{ padding: '12px 18px' }}>Files</th>
                  <th style={{ padding: '12px 18px' }}>Size</th>
                  <th style={{ padding: '12px 18px' }}>Status</th>
                  <th style={{ padding: '12px 18px' }}>Time</th>
                </tr>
              </thead>
              <tbody>
                {transfers.map((t) => {
                  const isDone = t.state === 'Completed';
                  return (
                    <tr
                      key={t.transfer_id}
                      style={{ borderBottom: '1px solid var(--border-subtle)' }}
                    >
                      <td style={{ padding: '12px 18px', fontWeight: 500, color: 'var(--text-primary)' }}>
                        {t.direction === 'send' ? 'Outgoing' : 'Incoming'}
                      </td>
                      <td style={{ padding: '12px 18px', color: 'var(--text-primary)' }}>
                        {t.peer_name}
                      </td>
                      <td style={{ padding: '12px 18px', color: 'var(--text-secondary)' }}>
                        {t.total_files}
                      </td>
                      <td style={{ padding: '12px 18px', color: 'var(--text-secondary)', fontFamily: 'JetBrains Mono, monospace' }}>
                        {(t.total_bytes / (1024 * 1024)).toFixed(2)} MB
                      </td>
                      <td style={{ padding: '12px 18px' }}>
                        <span style={{
                          fontSize: '11px',
                          fontWeight: 600,
                          padding: '2px 8px',
                          borderRadius: 'var(--radius-pill)',
                          backgroundColor: isDone ? 'rgba(52, 211, 153, 0.15)' : 'rgba(251, 191, 36, 0.15)',
                          color: isDone ? 'var(--success)' : 'var(--warning)'
                        }}>
                          {t.state}
                        </span>
                      </td>
                      <td style={{ padding: '12px 18px', color: 'var(--text-secondary)', fontSize: '12px' }}>
                        {new Date(t.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
};
