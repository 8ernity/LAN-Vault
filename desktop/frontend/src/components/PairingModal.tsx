import React, { useEffect, useRef, useState } from 'react';
import QRCode from 'qrcode';
import { X, RefreshCw, QrCode } from 'lucide-react';

interface PairingModalProps {
  isOpen: boolean;
  onClose: () => void;
  pairingQR: string;
  onRefreshQR: () => void;
}

export const PairingModal: React.FC<PairingModalProps> = ({
  isOpen,
  onClose,
  pairingQR,
  onRefreshQR,
}) => {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const [countdown, setCountdown] = useState<number>(120);

  useEffect(() => {
    if (canvasRef.current && pairingQR) {
      QRCode.toCanvas(canvasRef.current, pairingQR, {
        width: 220,
        margin: 1,
        color: {
          dark: '#000000',
          light: '#FFFFFF',
        },
      });
      setCountdown(120);
    }
  }, [pairingQR, isOpen]);

  useEffect(() => {
    if (!isOpen) return;
    const timer = setInterval(() => {
      setCountdown((prev) => {
        if (prev <= 1) {
          onRefreshQR();
          return 120;
        }
        return prev - 1;
      });
    }, 1000);
    return () => clearInterval(timer);
  }, [isOpen, onRefreshQR]);

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
        width: '380px',
        maxWidth: '90vw',
        boxShadow: '0 8px 32px rgba(0, 0, 0, 0.4)',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        padding: '24px',
        gap: '16px'
      }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <QrCode size={18} color="var(--accent-default)" />
            <span style={{ fontSize: '15px', fontWeight: 600, color: 'var(--text-primary)' }}>
              Pair New Device
            </span>
          </div>
          <button
            onClick={onClose}
            style={{ background: 'transparent', border: 'none', color: 'var(--text-secondary)', cursor: 'pointer' }}
          >
            <X size={18} />
          </button>
        </div>

        <p style={{ fontSize: '12px', color: 'var(--text-secondary)', textAlign: 'center', margin: 0 }}>
          Open LANVault on your Android phone or second PC and scan this QR code to authenticate mutual TLS.
        </p>

        <div style={{
          padding: '12px',
          backgroundColor: '#FFFFFF',
          borderRadius: 'var(--radius-control)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          boxShadow: '0 4px 12px rgba(0,0,0,0.1)'
        }}>
          <canvas ref={canvasRef} />
        </div>

        <div style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          width: '100%',
          fontSize: '12px',
          color: 'var(--text-secondary)'
        }}>
          <span>Refreshes in {countdown}s</span>
          <button
            onClick={onRefreshQR}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '4px',
              background: 'transparent',
              border: 'none',
              color: 'var(--accent-default)',
              fontSize: '12px',
              fontWeight: 600,
              cursor: 'pointer'
            }}
          >
            <RefreshCw size={14} />
            <span>Refresh</span>
          </button>
        </div>
      </div>
    </div>
  );
};
