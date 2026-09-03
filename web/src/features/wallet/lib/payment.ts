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
import {
  PAYMENT_TYPES,
  DEFAULT_PRESET_MULTIPLIERS,
  DEFAULT_PAYMENT_TYPE,
  DEFAULT_MIN_TOPUP,
} from '../constants'
import type { PaymentMethod, PresetAmount, TopupInfo } from '../types'

// ============================================================================
// Payment Processing Functions
// ============================================================================

/**
 * Check if browser is Safari
 */
function isSafariBrowser(): boolean {
  return (
    navigator.userAgent.includes('Safari') &&
    !navigator.userAgent.includes('Chrome')
  )
}

/**
 * Submit payment form (for non-Stripe payments)
 */
export function submitPaymentForm(
  url: string,
  params: Record<string, unknown>
): void {
  const form = document.createElement('form')
  form.action = url
  form.method = 'POST'

  // Don't open in new tab for Safari
  if (!isSafariBrowser()) {
    form.target = '_blank'
  }

  // Add form parameters
  Object.entries(params).forEach(([key, value]) => {
    const input = document.createElement('input')
    input.type = 'hidden'
    input.name = key
    input.value = String(value)
    form.appendChild(input)
  })

  document.body.appendChild(form)
  form.submit()
  document.body.removeChild(form)
}

/**
 * Check if payment method is Stripe
 */
export function isStripePayment(paymentType: string): boolean {
  return paymentType === PAYMENT_TYPES.STRIPE
}

/**
 * Check if payment method is Waffo
 */
export function isWaffoPayment(paymentType: string): boolean {
  return paymentType === PAYMENT_TYPES.WAFFO
}

/**
 * Check if payment method is Waffo Pancake
 *
 * Pancake is a metered-style payment that goes through a dedicated checkout
 * URL flow rather than the generic epay form submission, so it must be
 * special-cased in payment dispatch logic.
 */
export function isWaffoPancakePayment(paymentType: string): boolean {
  return paymentType === PAYMENT_TYPES.WAFFO_PANCAKE
}

/**
 * Check if payment method is XorPay QR scanning. XorPay returns a QR string
 * instead of a redirect URL, so it has a dedicated processor and dialog.
 */
export function isXorPayPayment(paymentType: string): boolean {
  return (
    paymentType === PAYMENT_TYPES.XORPAY_NATIVE ||
    paymentType === PAYMENT_TYPES.XORPAY_ALIPAY
  )
}

export interface PaymentProcessors {
  regular: (topupAmount: number, paymentType: string) => Promise<boolean>
  waffo: (topupAmount: number, payMethodIndex: number) => Promise<boolean>
  waffoPancake: (topupAmount: number) => Promise<boolean>
  /** XorPay opens a QR modal and polls the order; optional so existing callers stay valid */
  xorpay?: (topupAmount: number, paymentType: string) => Promise<boolean>
}

export async function dispatchSelectedPayment(
  paymentMethod: PaymentMethod,
  topupAmount: number,
  waffoMethodIndex: number | null,
  processors: PaymentProcessors
): Promise<boolean> {
  if (isWaffoPayment(paymentMethod.type)) {
    if (waffoMethodIndex === null) {
      return false
    }
    return processors.waffo(topupAmount, waffoMethodIndex)
  }

  if (isWaffoPancakePayment(paymentMethod.type)) {
    return processors.waffoPancake(topupAmount)
  }

  if (isXorPayPayment(paymentMethod.type)) {
    if (!processors.xorpay) {
      return false
    }
    return processors.xorpay(topupAmount, paymentMethod.type)
  }

  return processors.regular(topupAmount, paymentMethod.type)
}

/**
 * Get default payment type from topup info.
 *
 * When `amount` is provided, only returns a channel whose own minimum is
 * reachable (≤ amount). Without it, an enabled high-threshold channel (e.g.
 * Stripe min 10) used to be picked as default and the amount preview for a low
 * amount (e.g. XorPay at 1) would be rejected by the backend. Passing the
 * amount lets the no-selection preview land on a payable channel (XorPay).
 */
export function getDefaultPaymentType(
  topupInfo: TopupInfo | null,
  amount?: number
): string {
  if (!topupInfo) {
    return DEFAULT_PAYMENT_TYPE
  }

  const reachable = (min: number | undefined): boolean => {
    if (amount == null) return true
    const n = Number(min)
    return !(n > 0) || amount >= n
  }

  // Return first available payment method reachable by the amount
  if (topupInfo.pay_methods?.length > 0) {
    const hit = topupInfo.pay_methods.find((m) => reachable(m.min_topup))
    if (hit) {
      return hit.type
    }
  }

  if (topupInfo.enable_stripe_topup && reachable(topupInfo.stripe_min_topup)) {
    return PAYMENT_TYPES.STRIPE
  }

  if (topupInfo.enable_waffo_topup && reachable(topupInfo.waffo_min_topup)) {
    return PAYMENT_TYPES.WAFFO
  }

  if (
    topupInfo.enable_waffo_pancake_topup &&
    reachable(topupInfo.waffo_pancake_min_topup)
  ) {
    return PAYMENT_TYPES.WAFFO_PANCAKE
  }

  if (topupInfo.enable_xorpay_topup && reachable(topupInfo.xorpay_min_topup)) {
    return PAYMENT_TYPES.XORPAY_NATIVE
  }

  return topupInfo.pay_methods?.[0]?.type || DEFAULT_PAYMENT_TYPE
}

/**
 * Get minimum topup amount for the amount box: the lowest threshold among all
 * enabled channels. Previously this returned the first enabled channel's
 * threshold by priority, so an enabled Stripe (min 10) locked the whole amount
 * box to 10 and blocked low amounts through XorPay (min 1). Higher per-channel
 * thresholds are now handled by disabling that channel's own method buttons.
 */
export function getMinTopupAmount(topupInfo: TopupInfo | null): number {
  if (!topupInfo) {
    return DEFAULT_MIN_TOPUP
  }

  const minValues: number[] = []
  const pushMin = (value: number | undefined): void => {
    const n = Number(value)
    if (Number.isFinite(n) && n > 0) {
      minValues.push(n)
    }
  }

  if (topupInfo.enable_online_topup) {
    pushMin(topupInfo.min_topup)
  }
  if (topupInfo.enable_stripe_topup) {
    pushMin(topupInfo.stripe_min_topup)
  }
  if (topupInfo.enable_waffo_topup) {
    pushMin(topupInfo.waffo_min_topup)
  }
  if (topupInfo.enable_waffo_pancake_topup) {
    pushMin(topupInfo.waffo_pancake_min_topup)
  }
  if (topupInfo.enable_xorpay_topup) {
    pushMin(topupInfo.xorpay_min_topup)
  }

  return minValues.length > 0 ? Math.min(...minValues) : DEFAULT_MIN_TOPUP
}

/**
 * Generate preset amounts based on minimum topup
 */
export function generatePresetAmounts(minAmount: number): PresetAmount[] {
  return DEFAULT_PRESET_MULTIPLIERS.map((multiplier) => ({
    value: minAmount * multiplier,
  }))
}

/**
 * Merge custom preset amounts with discounts
 */
export function mergePresetAmounts(
  amountOptions: number[],
  discounts: Record<number, number>
): PresetAmount[] {
  if (!amountOptions || amountOptions.length === 0) {
    return []
  }

  return amountOptions.map((amount) => ({
    value: amount,
    discount: discounts[amount] || 1.0,
  }))
}
