using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading.Tasks;
using System.Windows.Input;

namespace Paymetheus
{
    public static class Commands
    {
        public static RoutedCommand CopyTxHash { get; } = new RoutedCommand();
    }
}
