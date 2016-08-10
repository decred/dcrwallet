// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Framework;
using System.Windows;
using System.Windows.Controls;

namespace Paymetheus
{
    /// <summary>
    /// Interaction logic for MainView.xaml
    /// </summary>
    public partial class ShellView : UserControl
    {
        private Navigator _navigator;
        private Overview _overview;
        private Request _request;

        public ShellView()
        {
            InitializeComponent();
            _navigator = Navigator.GetInstance(ContentHolder);
        }

        public void NavigateOverview(object sender, RoutedEventArgs e)
        {
            if (_overview == null)
            {
                _overview = new Overview();
                //_overview.DataContext = Shell._overviewViewModel;
            }
            NavigateTo(_overview);
        }

        public void NavigateAccounts(object sender, RoutedEventArgs e)
        {
            NavigateTo(new Accounts());
        }

        public void NavigateScripts(object sender, RoutedEventArgs e)
        {
            NavigateTo(new Scripts());
        }

        public void NavigateSend(object sender, RoutedEventArgs e)
        {
            NavigateTo(new Send());
        }

        public void NavigatePurchaseTickets(object sender, RoutedEventArgs e)
        {
            NavigateTo(new PurchaseTickets());
        }

        public void NavigateRequest(object sender, RoutedEventArgs e)
        {
            if (_request == null)
            {
                _request = new Request();
                //_request.DataContext = new RequestViewModel(Shell);
            }
            NavigateTo(_request);
        }

        public void NavigateHistory(object sender, RoutedEventArgs e)
        {
            NavigateTo(new History());
        }

        public void NavigateStakeMining(object sender, RoutedEventArgs e)
        {
            NavigateTo(new StakeMining());
        }

        public void NavigateUnspent(object sender, RoutedEventArgs e)
        {
            NavigateTo(new Unspent());
        }

        public void NavigateTo(Page page)
        {
            if(_navigator != null)
                _navigator.NavigateTo(page);
        }
    }
}
