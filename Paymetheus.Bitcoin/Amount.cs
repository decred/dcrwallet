// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;

namespace Paymetheus.Bitcoin
{
    public struct Amount
    {
        public Amount(long value)
        {
            val = value;
        }

        private long val;

        public static implicit operator Amount(long value) => new Amount { val = value };
        public static implicit operator long(Amount amount) => amount.val;

        public override string ToString() => Denomination.Bitcoin.FormatAmount(this);
    }

    public struct Denomination
    {
        public static readonly Denomination Bitcoin = new Denomination((long)1e8, "{0}{1:#,0}{2,-9:.0#######}", "BTC");
        public static readonly Denomination MillaBitcoin = new Denomination((long)1e5, "{0}{1:#,0}{2,-6:.0####}", "mBTC");
        public static readonly Denomination MicroBitcoin = new Denomination((long)1e2, "{0}{1:#,0}{2:.00}", "μBTC");

        private Denomination(long satoshisPerUnit, string formatString, string ticker)
        {
            _satoshisPerUnit = satoshisPerUnit;
            _formatString = formatString;

            Ticker = ticker;
        }

        private readonly long _satoshisPerUnit;
        private readonly string _formatString;

        public string Ticker { get; }

        public string FormatAmount(Amount amount)
        {
            // The negative sign has to be printed separately.  It can not be displayed as a
            // negative whole part, since there is no such thing as negative zero for longs.
            var negativeSign = "";
            var negative = amount < 0;
            if (negative)
            {
                amount = -amount;
                negativeSign = "-";
            }

            var wholePart = amount / _satoshisPerUnit;
            var decimalPart = (double)(amount % _satoshisPerUnit) / _satoshisPerUnit;

            return string.Format(_formatString, negativeSign, wholePart, decimalPart);
        }

        public Amount AmountFromString(string value)
        {
            if (value == null)
                throw new ArgumentNullException(nameof(value));

            var doubleValue = double.Parse(value);

            return AmountFromDouble(doubleValue);
        }

        public Amount AmountFromDouble(double value)
        {
            if (double.IsNaN(value) || double.IsInfinity(value))
                throw new ArgumentException("Invalid conversion from NaN or +-Infinity to Amount");

            return Round(value * _satoshisPerUnit);
        }

        private static Amount Round(double value)
        {
            if (value < 0)
                return (long)(value - 0.5);
            else
                return (long)(value + 0.5);
        }
    }
}