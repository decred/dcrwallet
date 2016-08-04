using Paymetheus.Decred;
using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading.Tasks;
using System.Windows.Data;
using System.Globalization;

namespace Paymetheus.Framework.ValueConverters
{
    public class AmountConverter : IValueConverter
    {
        public Denomination Denomination { get; set; } = Denomination.Decred;

        public object Convert(object value, Type targetType, object parameter, CultureInfo culture) =>
            Denomination.FormatAmount((Amount)value);

        public object ConvertBack(object value, Type targetType, object parameter, CultureInfo culture) =>
            Denomination.AmountFromString((string)value);
    }
}
