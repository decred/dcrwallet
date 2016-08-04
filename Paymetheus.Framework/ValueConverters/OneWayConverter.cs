using System;
using System.Collections.Generic;
using System.Globalization;
using System.Linq;
using System.Text;
using System.Threading.Tasks;
using System.Windows.Data;

namespace Paymetheus.Framework.ValueConverters
{
    public class OneWayConverter<TSource, TResult> : IValueConverter
    {
        Func<TSource, TResult> Converter;

        public OneWayConverter(Func<TSource, TResult> converter)
        {
            Converter = converter;
        }

        public object Convert(object value, Type targetType, object parameter, CultureInfo culture)
        {
            return Converter((TSource)value);
        }

        public object ConvertBack(object value, Type targetType, object parameter, CultureInfo culture)
        {
            throw new NotSupportedException();
        }
    }
}
