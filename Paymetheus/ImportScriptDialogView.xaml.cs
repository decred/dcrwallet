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
using System.Windows.Shapes;

namespace Paymetheus
{
    /// <summary>
    /// Interaction logic for ImportScript.xaml
    /// </summary>
    public partial class ImportScriptDialogView : UserControl
    {
        public ImportScriptDialogView()
        {
            InitializeComponent();
        }

        private void passphraseTextBox_PasswordChanged(object sender, RoutedEventArgs e)
        {
            if (DataContext != null)
            {
                ((dynamic)DataContext).Passphrase = ((PasswordBox)sender).Password;
            }
        }
    }
}
