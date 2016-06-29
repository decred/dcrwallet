using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Data;
using System.Windows.Documents;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using System.Windows.Navigation;
using System.Windows.Shapes;

namespace Paymetheus
{
    /// <summary>
    /// Interaction logic for Send.xaml
    /// </summary>
    public partial class Send : Page
    {
        public Send()
        {
            InitializeComponent();
        }

        private void OutputAmountTextBox_PreviewTextInput(object sender, TextCompositionEventArgs e)
        {
            e.Handled = e.Text.All(ch => !((ch >= '0' && ch <= '9') || ch == '.'));
        }
    }
}
