// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.Globalization;
using System.Linq;
using System.Text;
using System.Threading.Tasks;

namespace Paymetheus.Framework.TypeConverters
{
    public class AmountTypeConverter : TypeConverter
    {
        public override bool CanConvertFrom(ITypeDescriptorContext context, Type sourceType) => sourceType == typeof(string);

        public override bool CanConvertTo(ITypeDescriptorContext context, Type destinationType) => destinationType == typeof(Amount);

        public override object ConvertFrom(ITypeDescriptorContext context, CultureInfo culture, object value) =>
            Denomination.Decred.AmountFromString((string)value);

        public override object ConvertTo(ITypeDescriptorContext context, CultureInfo culture, object value, Type destinationType) =>
            Denomination.Decred.FormatAmount((Amount)value);
    }
}
