// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

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
    /// Interaction logic for PublicPassphrasePromptView.xaml
    /// </summary>
    public partial class PublicPassphrasePromptView : UserControl
    {
        public PublicPassphrasePromptView()
        {
            InitializeComponent();
        }

        private void PasswordBoxPublicPassphrase_PasswordChanged(object sender, RoutedEventArgs e)
        {
            if (DataContext != null)
            {
                ((dynamic)DataContext).PublicPassphrase = ((PasswordBox)sender).Password;
            }
        }
    }
}
