// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System.Windows;
using System.Windows.Controls;

namespace Paymetheus
{
    /// <summary>
    /// Interaction logic for MainWindow.xaml
    /// </summary>
    public partial class MainWindow : Window
    {
        public MainWindow()
        {
            InitializeComponent();
        }

        private void Window_Closed(object sender, System.EventArgs e)
        {
            Application.Current.Shutdown();
        }

        private void TextBoxPrivatePassphrase_PasswordChanged(object sender, RoutedEventArgs e)
        {
            if (DataContext != null)
            {
                ((dynamic)DataContext).ConnectTextBoxPrivatePassphrase = ((PasswordBox)sender).Password;
            }
        }
    }
}
