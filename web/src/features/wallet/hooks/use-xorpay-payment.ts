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
import i18next from 'i18next'
import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

import { getXorPayOrderStatus, isApiSuccess, requestXorPayPayment } from '../api'

// ============================================================================
// XorPay QR Payment Hook
// ============================================================================

export type XorPayPhase = 'loading' | 'waiting' | 'paid' | 'expired' | 'error'

const DEFAULT_EXPIRE_SECONDS = 900
const POLL_INTERVAL_MS = 2500

function getErrorMessage(message: string | undefined, data: unknown): string {
  if (typeof data === 'string' && data.trim()) {
    return data
  }
  return message || i18next.t('Payment request failed')
}

function parseExpire(raw: unknown): number {
  const n = Number(raw)
  return Number.isFinite(n) && n > 0 ? n : DEFAULT_EXPIRE_SECONDS
}

export interface XorPayState {
  open: boolean
  phase: XorPayPhase
  qrContent: string
  tradeNo: string
  methodName: string
  remainingSeconds: number
  /** True while the pay order request is in flight (before the QR is ready) */
  processing: boolean
  processXorPay: (
    topupAmount: number,
    paymentMethod: string,
    methodName?: string
  ) => Promise<boolean>
  closeXorPay: () => void
}

/**
 * Requests a XorPay order and drives a QR-code modal that keeps polling the
 * order status until it is paid or the QR expires. QR content is never opened
 * directly — the user scans it from a locally rendered image.
 *
 * `processXorPay` returns true only after the QR is ready; the caller then
 * closes the amount-confirm dialog and this hook opens the QR modal.
 */
export function useXorPayPayment(
  onPaid?: () => void | Promise<void>
): XorPayState {
  const [open, setOpen] = useState(false)
  const [phase, setPhase] = useState<XorPayPhase>('loading')
  const [qrContent, setQrContent] = useState('')
  const [tradeNo, setTradeNo] = useState('')
  const [methodName, setMethodName] = useState('')
  const [expireAt, setExpireAt] = useState(0)
  const [now, setNow] = useState(() => Date.now())
  const [processing, setProcessing] = useState(false)

  const onPaidRef = useRef(onPaid)
  onPaidRef.current = onPaid

  const closeXorPay = useCallback(() => {
    setOpen(false)
    setPhase('loading')
    setQrContent('')
    setTradeNo('')
    setExpireAt(0)
  }, [])

  // Keep latest tradeNo reachable from the polling interval without restarting
  // the interval on every render.
  const tradeNoRef = useRef('')

  const checkStatus = useCallback(async () => {
    const currentTradeNo = tradeNoRef.current
    if (!currentTradeNo) {
      return
    }
    try {
      const res = await getXorPayOrderStatus(currentTradeNo)
      const status =
        isApiSuccess(res) && res.data ? res.data.status : ''
      if (status === 'success') {
        setPhase('paid')
        void onPaidRef.current?.()
      } else if (status === 'failed' || status === 'expired') {
        setPhase('expired')
      }
    } catch {
      // Transient network error — retry on the next poll tick.
    }
  }, [])

  // Single interval while waiting: advances the countdown clock and polls.
  useEffect(() => {
    if (!open || phase !== 'waiting') {
      return
    }
    const interval = window.setInterval(() => {
      const current = Date.now()
      setNow(current)
      if (expireAt > 0 && current >= expireAt) {
        setPhase('expired')
        return
      }
      void checkStatus()
    }, POLL_INTERVAL_MS)
    return () => window.clearInterval(interval)
  }, [open, phase, expireAt, checkStatus])

  const processXorPay = useCallback(
    async (topupAmount: number, paymentMethod: string, name?: string) => {
      setMethodName(name || '')
      setProcessing(true)
      try {
        const response = await requestXorPayPayment({
          amount: Math.floor(topupAmount),
          payment_method: paymentMethod,
        })

        if (
          isApiSuccess(response) &&
          response.data &&
          typeof response.data !== 'string' &&
          response.data.qr_content
        ) {
          const data = response.data
          const base = Date.now()
          const expireSeconds = parseExpire(data.expire)
          tradeNoRef.current = data.trade_no || ''
          setQrContent(data.qr_content)
          setTradeNo(data.trade_no || '')
          setExpireAt(base + expireSeconds * 1000)
          setNow(base)
          setPhase('waiting')
          setOpen(true)
          return true
        }

        toast.error(getErrorMessage(response.message, response.data))
        return false
      } catch {
        toast.error(i18next.t('Payment request failed'))
        return false
      } finally {
        setProcessing(false)
      }
    },
    []
  )

  const remainingSeconds =
    expireAt > 0 ? Math.max(0, Math.ceil((expireAt - now) / 1000)) : 0

  return {
    open,
    phase,
    qrContent,
    tradeNo,
    methodName,
    remainingSeconds,
    processing,
    processXorPay,
    closeXorPay,
  }
}
