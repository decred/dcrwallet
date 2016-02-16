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
using System.Windows.Controls.Primitives;
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
    /// Interaction logic for MainView.xaml
    /// </summary>
    public partial class ShellView : UserControl
    {
        public ShellView()
        {
            InitializeComponent();
        }

        private LockableToggleButton _toggledSidebarButton;

        private bool ReplaceCheckedSidebarButton(RoutedEventArgs e, LockableToggleButton replacement)
        {
            if (_toggledSidebarButton != replacement)
            {
                if (_toggledSidebarButton != null)
                {
                    _toggledSidebarButton.Locked = false;
                    _toggledSidebarButton.IsChecked = false;
                }
                _toggledSidebarButton = replacement;
                _toggledSidebarButton.Locked = true;
                return true;
            }
            else
            {
                return false;
            }
        }

        private void RecentActivityToggleButton_Checked(object sender, RoutedEventArgs e)
        {
            var button = (LockableToggleButton)e.Source;
            if (ReplaceCheckedSidebarButton(e, button))
            {
                Console.WriteLine("todo");
            }
        }

        private void AccountToggleButton_Checked(object sender, RoutedEventArgs e)
        {
            var button = (LockableToggleButton)e.Source;
            if (ReplaceCheckedSidebarButton(e, button))
            {
                var selected = (RecentAccountViewModel)button.DataContext;
                if (selected != null)
                {
                    selected.PostMessage(new ShowAccountMessage(selected.Account));
                }
            }
        }
    }
}
