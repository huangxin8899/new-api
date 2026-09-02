/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { QRCodeSVG } from 'qrcode.react'
import { useTranslation } from 'react-i18next'

import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'

import type { XorPayPhase } from '../../hooks'

interface XorPayQrDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  phase: XorPayPhase
  qrContent: string
  methodName: string
  tradeNo: string
  remainingSeconds: number
}

function formatCountdown(totalSeconds: number): string {
  const safe = Math.max(0, totalSeconds)
  const minutes = Math.floor(safe / 60)
  const seconds = safe % 60
  return `${minutes}:${String(seconds).padStart(2, '0')}`
}

/**
 * Shows a locally rendered QR code for XorPay WeChat / Alipay scanning and
 * reflects the polling state driven by useXorPayPayment (waiting / paid /
 * expired). The QR string is never navigated to — it is only encoded as an
 * image for the user to scan.
 */
export function XorPayQrDialog({
  open,
  onOpenChange,
  phase,
  qrContent,
  methodName,
  tradeNo,
  remainingSeconds,
}: XorPayQrDialogProps) {
  const { t } = useTranslation()

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-sm'>
        <AlertDialogHeader>
          <AlertDialogTitle className='text-xl font-semibold'>
            {t('Scan to Pay')}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t('Scan with {{method}} to finish the payment.', {
              method: methodName,
            })}
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className='flex flex-col items-center gap-3 py-2 text-center sm:py-3'>
          {phase === 'waiting' && qrContent ? (
            <>
              <div className='rounded-lg bg-white p-2 shadow-sm ring-1 ring-border'>
                <QRCodeSVG
                  value={qrContent}
                  size={200}
                  level='M'
                  bgColor='#FFFFFF'
                  fgColor='#000000'
                />
              </div>
              <p className='text-muted-foreground text-sm'>
                {t('QR code expires in {{time}}', {
                  time: formatCountdown(remainingSeconds),
                })}
              </p>
            </>
          ) : null}

          {phase === 'paid' ? (
            <p className='py-4 text-center text-base font-semibold text-green-600'>
              {t('Payment received. Credits have been added.')}
            </p>
          ) : null}

          {phase === 'expired' ? (
            <p className='text-muted-foreground py-4 text-center text-sm'>
              {t('This QR code has expired. Start a new payment.')}
            </p>
          ) : null}

          {phase === 'waiting' ? (
            <p className='text-muted-foreground text-xs'>{t('Waiting for payment...')}</p>
          ) : null}

          {tradeNo ? (
            <p className='text-muted-foreground w-full break-all text-xs'>
              {t('Order No.')}: {tradeNo}
            </p>
          ) : null}
        </div>

        <AlertDialogFooter className='sm:justify-center'>
          <AlertDialogCancel>{t('Close')}</AlertDialogCancel>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
