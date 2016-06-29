using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading.Tasks;
using System.Windows;

namespace Paymetheus.Framework.ValueConverters
{
    public class NullToHiddenConverter : OneWayConverter<object, Visibility>
    {
        public NullToHiddenConverter() : base(o => o == null ? Visibility.Hidden : Visibility.Visible) { }
    }
}
