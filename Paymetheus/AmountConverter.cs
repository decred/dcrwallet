// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using System;
using System.Globalization;
using System.Windows.Data;

namespace Paymetheus
{
    sealed class AmountConverter : IValueConverter
    {
        public object Convert(object value, Type targetType, object parameter, CultureInfo culture)
        {
            if (value == null)
                return "";
            else
                return ((Amount)value).ToString();
        }

        public object ConvertBack(object value, Type targetType, object parameter, CultureInfo culture)
        {
            double fpValue;
            if (!double.TryParse((string)value, out fpValue))
                return null;
            if (double.IsNaN(fpValue) || double.IsInfinity(fpValue))
                return null;

            return Denomination.Decred.AmountFromDouble(fpValue);
        }
    }
}
