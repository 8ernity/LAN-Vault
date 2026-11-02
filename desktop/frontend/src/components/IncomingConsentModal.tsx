import React from 'react';
import { Download, ShieldCheck, X, Check } from 'lucide-react';

export interface IncomingTransferOffer {
  transfer_id: string;
  sender_name: string;
  sender_fp: string;
  file_count: number;
  total_bytes: number;
  short_fp: string;
}

interface IncomingConsentModalProps {
  offer: IncomingTransferOffer | null;
  onRespond: (transferID: string, accept: boolean) => void;
}

export const IncomingConsentModal: React.FC<IncomingConsentModalProps> = ({
  offer,
  onRespond,
}) => {
  if (!offer) return null;

  return (
    <div style={{
      position: 'fixed',
      top: 0,
      left: 0,
      width: '100vw',
      height: '100vh',
      backgroundColor: 'rgba(0, 0, 0, 0.7)',
      backdropFilter: 'blur(5px)',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      zIndex: 1100,
    }}>
      <div style={{
        backgroundColor: 'var(--bg-surface)',
        borderRadius: 'var(--radius-dialog)',
        border: '1px solid var(--accent-default)',
        width: '440px',
        maxWidth: '90vw',
        boxShadow: '0 12px 40px rgba(0, 0, 0, 0.5)',
        display: 'flex',
        flexDirection: 'column',
        padding: '24px',
        gap: '20px',
        animation: 'fadeIn 200ms ease-out'
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
            <Download size={24} />
          </div>
          <div>
            <h2 style={{ fontSize: '16px', fontWeight: 600, color: 'var(--text-primary)' }}>
              Incoming Transfer Request
            </h2>
            <div style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>
              From <strong style={{ color: 'var(--text-primary)' }}>{offer.sender_name}</strong>
            </div>
          </div>
        </div>

        {/* Offer Details */}
        <div style={{
          backgroundColor: 'var(--bg-base)',
          borderRadius: 'var(--radius-control)',
          border: '1px solid var(--border-subtle)',
          padding: '14px 16px',
          display: 'flex',
          flexDirection: 'column',
          gap: '8px',
          fontSize: '13px'
        }}>
          <div style={{ display: 'flex', justifyContent: 'space-between' }}>
            <span style={{ color: 'var(--text-secondary)' }}>Files</span>
            <strong style={{ color: 'var(--text-primary)' }}>{offer.file_count} item(s)</strong>
          </div>
          <div style={{ display: 'flex', justifyContent: 'space-between' }}>
            <span style={{ color: 'var(--text-secondary)' }}>Total size</span>
            <strong style={{ color: 'var(--text-primary)', fontFamily: 'JetBrains Mono, monospace' }}>
              {(offer.total_bytes / (1024 * 1024)).toFixed(2)} MB
            </strong>
          </div>
          <div style={{ display: 'flex', justifyContent: 'space-between' }}>
            <span style={{ color: 'var(--text-secondary)' }}>Fingerprint</span>
            <span style={{ fontFamily: 'JetBrains Mono, monospace', fontSize: '12px', color: 'var(--text-primary)' }}>
              {offer.short_fp}
            </span>
          </div>
        </div>

        <p style={{ fontSize: '12px', color: 'var(--text-secondary)', margin: 0 }}>
          Files will be safely stored under your Downloads\LANVault folder upon acceptance.
        </p>

        {/* Action Buttons */}
        <div style={{ display: 'flex', gap: '12px' }}>
          <button
            onClick={() => onRespond(offer.transfer_id, false)}
            style={{
              flex: 1,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              gap: '6px',
              padding: '10px',
              backgroundColor: 'var(--bg-raised)',
              border: '1px solid var(--border-subtle)',
              borderRadius: 'var(--radius-control)',
              color: 'var(--text-primary)',
              fontSize: '13px',
              fontWeight: 600,
              cursor: 'pointer'
            }}
          >
            <X size={16} />
            <span>Decline</span>
          </button>

          <button
            onClick={() => onRespond(offer.transfer_id, true)}
            style={{
              flex: 1,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              gap: '6px',
              padding: '10px',
              backgroundColor: 'var(--accent-default)',
              color: 'var(--accent-onAccent)',
              border: 'none',
              borderRadius: 'var(--radius-control)',
              fontSize: '13px',
              fontWeight: 600,
              cursor: 'pointer',
              boxShadow: '0 2px 8px rgba(76, 141, 255, 0.3)'
            }}
          >
            <Check size={16} />
            <span>Accept</span>
          </button>
        </div>
      </div>
    </div>
  );
};
