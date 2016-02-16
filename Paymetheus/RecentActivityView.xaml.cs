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
    /// Interaction logic for RecentActivityView.xaml
    /// </summary>
    public partial class RecentActivityView : UserControl
    {
        public RecentActivityView()
        {
            InitializeComponent();
        }

        private void transactionSummaries_SelectionChanged(object sender, SelectionChangedEventArgs e)
        {
            var listView = (ListView)sender;
            var selected = (TransactionViewModel)listView.SelectedValue;
            if (selected != null)
            {
                var viewModel = (RecentActivityViewModel)DataContext;
                viewModel.PostMessage(new ShowTransactionMessage(selected));
            }
        }
    }
}
