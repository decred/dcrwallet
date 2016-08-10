// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;

namespace Paymetheus.Decred
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

        public override string ToString() => Denomination.Decred.FormatAmount(this);
    }

    public struct Denomination
    {
        public static readonly Denomination Decred = new Denomination((long)1e8, 8, "{0}{1:#,0}{2,-2:.0#######}", "DCR");
        public static readonly Denomination MillaDecred = new Denomination((long)1e5, 5, "{0}{1:#,0}{2,-2:.0####}", "mDCR");
        public static readonly Denomination MicroDecred = new Denomination((long)1e2, 2, "{0}{1:#,0}{2:.00}", "μDCR");

        private Denomination(long atomsPerUnit, int decimalPoints, string formatString, string ticker)
        {
            _atomsPerUnit = atomsPerUnit;
            _formatString = formatString;

            DecimalPoints = decimalPoints;
            Ticker = ticker;
        }

        private readonly long _atomsPerUnit;
        private readonly string _formatString;

        public int DecimalPoints { get; }
        public string Ticker { get; }

        public Tuple<long, double> Split(Amount amount)
        {
            if (amount < 0)
            {
                amount = -amount;
            }

            var wholePart = amount / _atomsPerUnit;
            var decimalPart = (double)(amount % _atomsPerUnit) / _atomsPerUnit;
            return Tuple.Create(wholePart, decimalPart);
        }

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

            var wholePart = amount / _atomsPerUnit;
            var decimalPart = (double)(amount % _atomsPerUnit) / _atomsPerUnit;

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

            return Round(value * _atomsPerUnit);
        }

        public double DoubleFromAmount(Amount amount)
        {
            return (double)amount / _atomsPerUnit;
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