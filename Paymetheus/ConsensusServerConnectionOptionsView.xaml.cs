// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Rpc;
using System;
using System.IO;
using System.Windows;
using System.Windows.Controls;

namespace Paymetheus
{
    /// <summary>
    /// Interaction logic for UserControl1.xaml
    /// </summary>
    public partial class ConsensusServerConnectionOptionsView : UserControl
    {
        public ConsensusServerConnectionOptionsView()
        {
            InitializeComponent();
        }

        private void TextBoxConsensusServerRpcPassword_PasswordChanged(object sender, RoutedEventArgs e)
        {
            if (DataContext != null)
            {
                ((dynamic)DataContext).ConsensusServerRpcPassword = ((PasswordBox)sender).Password;
            }
        }

        private void Button_Click(object sender, RoutedEventArgs e)
        {
            if (DataContext == null)
            {
                return;
            }

            var certificateFile = ((dynamic)DataContext).ConsensusServerCertificateFile;

            var fileDialog = new Microsoft.Win32.OpenFileDialog();
            fileDialog.Title = $"Select {ConsensusServerRpcOptions.ApplicationName} RPC certificate";
            if (!string.IsNullOrWhiteSpace(certificateFile))
            {
                fileDialog.InitialDirectory = Path.GetDirectoryName(certificateFile);
                fileDialog.FileName = Path.GetFileName(certificateFile);
            }
            if (fileDialog.ShowDialog() ?? false)
            {
                ((dynamic)DataContext).ConsensusServerCertificateFile = fileDialog.FileName;
            }
        }
    }
}
